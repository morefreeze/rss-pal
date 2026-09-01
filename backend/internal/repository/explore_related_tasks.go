package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	ExploreTaskDiscoverRelated = "discover_related"
	ExplorePriorityRelatedSeed = ExplorePriorityRelated
	maxExploreRelatedScanPage  = 250
)

type ExploreRelatedProduceResult struct {
	ScannedFeeds    int
	ScannedArticles int
	Enqueued        int
}

type ExploreRelatedTaskRepository struct {
	db *sql.DB
}

func NewExploreRelatedTaskRepository(db *sql.DB) *ExploreRelatedTaskRepository {
	return &ExploreRelatedTaskRepository{db: db}
}

// Produce performs two bounded keyset scans. Empty pages wrap their own
// cursor and immediately scan the first page, so finite tables keep cycling
// while appended rows are reached before the next wrap.
func (r *ExploreRelatedTaskRepository) Produce(ctx context.Context, now time.Time, pageSize int) (ExploreRelatedProduceResult, error) {
	result := ExploreRelatedProduceResult{}
	if r == nil || r.db == nil {
		return result, errors.New("explore related producer requires a database")
	}
	if pageSize <= 0 || pageSize > maxExploreRelatedScanPage {
		pageSize = maxExploreRelatedScanPage
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var providerID, feedCursor, articleCursor int
	if err := tx.QueryRowContext(ctx, `
		SELECT provider.id,state.feed_cursor,state.article_cursor
		FROM explore_related_scan_state state
		JOIN explore_registry_providers provider
		  ON provider.provider_key='related-sites' AND provider.provider_kind='related_site' AND provider.enabled
		WHERE state.id=1 FOR UPDATE OF state`).Scan(&providerID, &feedCursor, &articleCursor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, tx.Commit()
		}
		return result, err
	}
	feedRows, nextFeed, err := loadRelatedFeedPage(ctx, tx, feedCursor, pageSize)
	if err != nil {
		return result, err
	}
	if len(feedRows) == 0 && feedCursor != 0 {
		feedRows, nextFeed, err = loadRelatedFeedPage(ctx, tx, 0, pageSize)
		if err != nil {
			return result, err
		}
	}
	articleRows, nextArticle, err := loadRelatedArticlePage(ctx, tx, articleCursor, now.Add(-30*24*time.Hour), pageSize)
	if err != nil {
		return result, err
	}
	if len(articleRows) == 0 && articleCursor != 0 {
		articleRows, nextArticle, err = loadRelatedArticlePage(ctx, tx, 0, now.Add(-30*24*time.Hour), pageSize)
		if err != nil {
			return result, err
		}
	}
	result.ScannedFeeds, result.ScannedArticles = len(feedRows), len(articleRows)
	seen := make(map[string]struct{}, len(feedRows)+len(articleRows))
	for _, row := range append(feedRows, articleRows...) {
		canonical, ok := canonicalExploreRelatedSeedURL(row.url)
		if !ok {
			continue
		}
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		changed, err := enqueueRelatedSeed(ctx, tx, providerID, canonical, ExplorePriorityRelatedSeed)
		if err != nil {
			return result, err
		}
		if changed {
			result.Enqueued++
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE explore_related_scan_state SET feed_cursor=$1,article_cursor=$2,updated_at=$3 WHERE id=1`, nextFeed, nextArticle, now); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

type relatedSeedRow struct {
	id  int
	url string
}

func loadRelatedFeedPage(ctx context.Context, tx *sql.Tx, cursor, limit int) ([]relatedSeedRow, int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT feed.id,COALESCE(source.site_url,feed.url)
		FROM feeds feed
		LEFT JOIN recommended_feeds source ON source.normalized_url=lower(btrim(feed.url)) AND source.merged_into_source_id IS NULL
		WHERE feed.id>$1 AND feed.status='active' AND feed.is_active
		ORDER BY feed.id ASC LIMIT $2`, cursor, limit)
	if err != nil {
		return nil, cursor, err
	}
	defer rows.Close()
	result, next := []relatedSeedRow{}, cursor
	for rows.Next() {
		var row relatedSeedRow
		if err := rows.Scan(&row.id, &row.url); err != nil {
			return nil, cursor, err
		}
		result, next = append(result, row), row.id
	}
	return result, next, rows.Err()
}

func loadRelatedArticlePage(ctx context.Context, tx *sql.Tx, cursor int, since time.Time, limit int) ([]relatedSeedRow, int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT article.id,article.url
		FROM articles article JOIN feeds feed ON feed.id=article.feed_id
		WHERE article.id>$1 AND feed.status='active' AND feed.is_active
		  AND COALESCE(article.published_at,article.fetched_at)>=$2
		ORDER BY article.id ASC LIMIT $3`, cursor, since, limit)
	if err != nil {
		return nil, cursor, err
	}
	defer rows.Close()
	result, next := []relatedSeedRow{}, cursor
	for rows.Next() {
		var row relatedSeedRow
		if err := rows.Scan(&row.id, &row.url); err != nil {
			return nil, cursor, err
		}
		result, next = append(result, row), row.id
	}
	return result, next, rows.Err()
}

func enqueueRelatedSeed(ctx context.Context, tx *sql.Tx, providerID int, canonical string, priority int) (bool, error) {
	var id int
	err := tx.QueryRowContext(ctx, `
		INSERT INTO explore_related_tasks(provider_id,canonical_seed_url,priority)
		VALUES ($1,$2,$3)
		ON CONFLICT (canonical_seed_url) WHERE status IN ('pending','leased')
		DO UPDATE SET priority=EXCLUDED.priority,updated_at=CURRENT_TIMESTAMP
		RETURNING id`, providerID, canonical, sanitizeExplorePriority(priority)).Scan(&id)
	return err == nil, err
}
