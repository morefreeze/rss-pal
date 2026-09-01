package repository

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"

	explorelogic "github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/util"
	"github.com/lib/pq"
)

const exploreSubscriptionProfileSQL = `
	SELECT COALESCE(feed.title,''),feed.url,
	       COALESCE((
	           SELECT article.category
	           FROM articles article
	           WHERE article.feed_id=feed.id AND NULLIF(btrim(article.category),'') IS NOT NULL
	             AND ((feed.owner_id=$1 AND COALESCE(article.published_at,article.fetched_at) >= $2)
	               OR (feed.owner_id IS NULL AND article.published_at IS NOT NULL
	                   AND article.published_at >= GREATEST($2,profile_user.shared_visible_from)))
	           GROUP BY article.category
	           ORDER BY COUNT(*) DESC,MAX(COALESCE(article.published_at,article.fetched_at)) DESC,article.category
	           LIMIT 1
	       ),''),
	       ARRAY(
	           SELECT ranked_tag.tag FROM (
	               SELECT btrim(tag) AS tag
	               FROM articles article
	               CROSS JOIN LATERAL unnest(COALESCE(article.tags,'{}')) tag
	               WHERE article.feed_id=feed.id AND NULLIF(btrim(tag),'') IS NOT NULL
	                 AND ((feed.owner_id=$1 AND COALESCE(article.published_at,article.fetched_at) >= $2)
	                   OR (feed.owner_id IS NULL AND article.published_at IS NOT NULL
	                       AND article.published_at >= GREATEST($2,profile_user.shared_visible_from)))
	               GROUP BY btrim(tag)
	               ORDER BY COUNT(*) DESC,MAX(COALESCE(article.published_at,article.fetched_at)) DESC,btrim(tag)
	               LIMIT 20
	           ) ranked_tag
	       )
	FROM feeds feed
	JOIN users profile_user ON profile_user.id=$1
	WHERE feed.owner_id IS NULL OR feed.owner_id=$1
	ORDER BY feed.id`

const exploreRecentArticleProfileSQL = `
	SELECT article.title, COALESCE(article.category,''), COALESCE(article.topic,''),
	       COALESCE(article.tags,'{}'), LEFT(COALESCE(article.content,''),4000),
	       LEFT(COALESCE(article.summary_brief,''),1000), COALESCE(article.published_at,article.fetched_at)
	FROM articles article
	JOIN feeds feed ON feed.id=article.feed_id
	JOIN users profile_user ON profile_user.id=$1
	WHERE (feed.owner_id=$1 AND COALESCE(article.published_at,article.fetched_at) >= $2)
	   OR (feed.owner_id IS NULL AND article.published_at IS NOT NULL
	       AND article.published_at >= GREATEST($2,profile_user.shared_visible_from))
	ORDER BY COALESCE(article.published_at,article.fetched_at) DESC, article.id DESC LIMIT 200`

// ExploreProfileSignalRepository loads the formal subscription and recent
// article inputs shared by worker and request-time cold snapshot generation.
// When transaction-bound, PostgreSQL RLS remains an additional visibility
// boundary on top of the explicit worker-safe shared article floor.
type ExploreProfileSignalRepository struct{ db Querier }

func NewExploreProfileSignalRepository(db Querier) *ExploreProfileSignalRepository {
	return &ExploreProfileSignalRepository{db: db}
}

func (repository *ExploreProfileSignalRepository) Load(ctx context.Context, userID int, now time.Time) (explorelogic.ProfileInput, error) {
	profile := explorelogic.ProfileInput{Now: now}
	recentSince := now.Add(-30 * 24 * time.Hour)
	rows, err := repository.db.QueryContext(ctx, exploreSubscriptionProfileSQL, userID, recentSince)
	if err != nil {
		return profile, err
	}
	indexesByURL := make(map[string][]int)
	for rows.Next() {
		var item explorelogic.SubscriptionSignalInput
		var rawURL string
		if err := rows.Scan(&item.Title, &rawURL, &item.Category, pq.Array(&item.Tags)); err != nil {
			rows.Close()
			return profile, err
		}
		item.Domain = exploreProfileURLDomain(rawURL)
		normalizedURL := util.NormalizeURL(strings.TrimSpace(rawURL))
		if normalizedURL != "" {
			indexesByURL[normalizedURL] = append(indexesByURL[normalizedURL], len(profile.Subscriptions))
		}
		profile.Subscriptions = append(profile.Subscriptions, item)
	}
	if err := closeExploreProfileRows(rows); err != nil {
		return profile, err
	}
	if len(indexesByURL) > 0 {
		normalizedURLs := make([]string, 0, len(indexesByURL))
		for normalizedURL := range indexesByURL {
			normalizedURLs = append(normalizedURLs, normalizedURL)
		}
		sort.Strings(normalizedURLs)
		rows, err = repository.db.QueryContext(ctx, `
			SELECT id,normalized_url FROM recommended_feeds
			WHERE normalized_url=ANY($1)
			ORDER BY id`, pq.Array(normalizedURLs))
		if err != nil {
			return profile, err
		}
		for rows.Next() {
			var sourceID int
			var normalizedURL string
			if err := rows.Scan(&sourceID, &normalizedURL); err != nil {
				rows.Close()
				return profile, err
			}
			for _, index := range indexesByURL[normalizedURL] {
				profile.Subscriptions[index].SourceID = sourceID
			}
		}
		if err := closeExploreProfileRows(rows); err != nil {
			return profile, err
		}
	}

	rows, err = repository.db.QueryContext(ctx, exploreRecentArticleProfileSQL, userID, recentSince)
	if err != nil {
		return profile, err
	}
	defer rows.Close()
	for rows.Next() {
		var item explorelogic.RecentArticleSignalInput
		var content, snippet string
		if err := rows.Scan(&item.Title, &item.Category, &item.Topic, pq.Array(&item.Tags), &content, &snippet, &item.PublishedAt); err != nil {
			return profile, err
		}
		item.TextTokens = explorelogic.ProfileTextTokens(content, snippet)
		profile.RecentArticles = append(profile.RecentArticles, item)
	}
	return profile, rows.Err()
}

func closeExploreProfileRows(rows interface {
	Err() error
	Close() error
}) error {
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	return rows.Close()
}

func exploreProfileURLDomain(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
