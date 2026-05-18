package backup

import (
	"context"
	"database/sql"
	"fmt"
)

// RestoreStats summarizes what changed during a Restore call.
//
// SavedArticles, ReadingProgress, ArticleUserTags, UserPreferences count the
// rows the restore flow processed (insert-or-no-op-on-conflict). Existing
// rows are intentionally preserved on conflict — "DB wins".
//
// SkippedMissingFeed is incremented when a SavedArticleRow's FeedURL doesn't
// resolve to any feed in the restored DB (should not happen for an internally
// consistent backup).
//
// SkippedArticleLink counts ArticleUserTag / UserPreference rows whose
// article_id is NOT in the saved set — they reference non-backed-up RSS
// articles and remain unrestorable.
type RestoreStats struct {
	Feeds              int `json:"feeds"`
	UserTags           int `json:"user_tags"`
	InterestCategories int `json:"interest_categories"`
	InterestTopics     int `json:"interest_topics"`
	SavedArticles      int `json:"saved_articles"`
	ReadingProgress    int `json:"reading_progress"`
	ArticleUserTags    int `json:"article_user_tags"`
	UserPreferences    int `json:"user_preferences"`
	SkippedArticleLink int `json:"skipped_article_link"`
	SkippedMissingFeed int `json:"skipped_missing_feed"`
}

// Restore applies a backup pair (metadata + saved) to the DB in one TX.
// ss may be nil (legacy backup with no sibling); in that case behavior
// matches the pre-saved-snapshot version: saved articles aren't restored
// and every ArticleUserTag / UserPreference goes to SkippedArticleLink.
//
// Restore is additive — existing rows are preserved on conflict, so re-running
// it over an already-restored DB is a no-op for existing rows.
func Restore(ctx context.Context, db *sql.DB, s *Snapshot, ss *SavedSnapshot) (RestoreStats, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RestoreStats{}, err
	}
	defer tx.Rollback()

	var stats RestoreStats

	for i := range s.Feeds {
		f := &s.Feeds[i]
		_, err := tx.ExecContext(ctx, `
			INSERT INTO feeds (url, title, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (url) DO UPDATE SET
				title = EXCLUDED.title,
				fetch_interval_minutes = EXCLUDED.fetch_interval_minutes,
				is_active = EXCLUDED.is_active,
				owner_id = EXCLUDED.owner_id,
				feed_type = EXCLUDED.feed_type,
				status = EXCLUDED.status,
				priority_weight = EXCLUDED.priority_weight`,
			f.URL, f.Title, f.FetchIntervalMin, f.ETag, f.LastModified, f.IsActive,
			ownerOrNil(f.OwnerID), f.FeedType, f.Status, f.PriorityWeight, f.CreatedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert feed %s: %w", f.URL, err)
		}
		stats.Feeds++
	}

	for i := range s.UserTags {
		t := &s.UserTags[i]
		_, err := tx.ExecContext(ctx, `
			INSERT INTO user_tags (user_id, name, created_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (user_id, name) DO NOTHING`,
			t.UserID, t.Name, t.CreatedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert user_tag (%d,%s): %w", t.UserID, t.Name, err)
		}
		stats.UserTags++
	}

	for i := range s.InterestCategories {
		c := &s.InterestCategories[i]
		_, err := tx.ExecContext(ctx, `
			INSERT INTO interest_categories (user_id, category, weight, last_reinforced_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id, category) DO UPDATE SET
				weight = EXCLUDED.weight,
				last_reinforced_at = EXCLUDED.last_reinforced_at`,
			c.UserID, c.Category, c.Weight, c.LastReinforcedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert interest_category (%d,%s): %w", c.UserID, c.Category, err)
		}
		stats.InterestCategories++
	}

	for i := range s.InterestTopics {
		t := &s.InterestTopics[i]
		_, err := tx.ExecContext(ctx, `
			INSERT INTO interest_topics (user_id, topic, weight, last_reinforced_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id, topic) DO UPDATE SET
				weight = EXCLUDED.weight,
				last_reinforced_at = EXCLUDED.last_reinforced_at`,
			ownerOrNil(t.UserID), t.Topic, t.Weight, t.LastReinforcedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert interest_topic %s: %w", t.Topic, err)
		}
		stats.InterestTopics++
	}

	// Saved articles + id map for downstream relation restoration.
	idMap := make(map[int]int)
	if ss != nil {
		feedIDByURL, err := loadFeedIDByURL(ctx, tx)
		if err != nil {
			return stats, fmt.Errorf("load feed id map: %w", err)
		}
		for i := range ss.SavedArticles {
			ar := &ss.SavedArticles[i]
			feedID, ok := feedIDByURL[ar.FeedURL]
			if !ok {
				stats.SkippedMissingFeed++
				continue
			}
			var newID int
			err := tx.QueryRowContext(ctx, `
				INSERT INTO articles (
					feed_id, title, url, content, published_at,
					summary_brief, summary_detailed, fetched_at,
					word_count, reading_minutes, editor_note,
					media_url, media_type, media_duration_seconds
				) VALUES ($1,$2,$3,$4,$5, $6,$7,$8, $9,$10,$11, $12,$13,$14)
				ON CONFLICT (feed_id, url) WHERE parent_article_id IS NULL
				DO NOTHING
				RETURNING id`,
				feedID, ar.Title, ar.URL, ar.Content, ar.PublishedAt,
				ar.SummaryBrief, ar.SummaryDetailed, ar.FetchedAt,
				ar.WordCount, ar.ReadingMinutes, ar.EditorNote,
				ar.MediaURL, ar.MediaType, ar.MediaDurationSeconds,
			).Scan(&newID)
			if err == sql.ErrNoRows {
				// Conflict — row exists; look it up.
				err = tx.QueryRowContext(ctx, `
					SELECT id FROM articles
					WHERE feed_id = $1 AND url = $2 AND parent_article_id IS NULL`,
					feedID, ar.URL).Scan(&newID)
			}
			if err != nil {
				return stats, fmt.Errorf("upsert saved article %s: %w", ar.URL, err)
			}
			idMap[ar.ExportID] = newID
			stats.SavedArticles++
		}

		for i := range ss.ReadingProgress {
			rp := &ss.ReadingProgress[i]
			newAID, ok := idMap[rp.ArticleExportID]
			if !ok {
				continue
			}
			_, err := tx.ExecContext(ctx, `
				INSERT INTO reading_progress (user_id, article_id, scroll_position, last_read_at, is_completed)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (article_id, user_id) DO NOTHING`,
				rp.UserID, newAID, rp.ScrollPosition, rp.LastReadAt, rp.IsCompleted)
			if err != nil {
				return stats, fmt.Errorf("upsert reading_progress (%d,%d): %w", rp.UserID, newAID, err)
			}
			stats.ReadingProgress++
		}
	}

	for i := range s.ArticleUserTags {
		row := &s.ArticleUserTags[i]
		newAID, ok := idMap[row.ArticleID]
		if !ok {
			stats.SkippedArticleLink++
			continue
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO article_user_tags (article_id, tag_id, user_id, created_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (article_id, tag_id) DO NOTHING`,
			newAID, row.TagID, row.UserID, row.CreatedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert article_user_tag (%d,%d): %w", newAID, row.TagID, err)
		}
		stats.ArticleUserTags++
	}

	// user_preferences has no general unique constraint, so use WHERE NOT
	// EXISTS for idempotence. Safe within the restore TX (no concurrent
	// writes to these rows).
	for i := range s.UserPreferences {
		p := &s.UserPreferences[i]
		newAID, ok := idMap[p.ArticleID]
		if !ok {
			stats.SkippedArticleLink++
			continue
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO user_preferences (user_id, article_id, signal_type, signal_value, created_at)
			SELECT $1, $2, $3, $4, $5
			WHERE NOT EXISTS (
				SELECT 1 FROM user_preferences
				WHERE user_id = $1 AND article_id = $2 AND signal_type = $3
			)`,
			p.UserID, newAID, p.SignalType, p.SignalValue, p.CreatedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert user_preference (%d,%d,%s): %w", p.UserID, newAID, p.SignalType, err)
		}
		stats.UserPreferences++
	}

	if err := tx.Commit(); err != nil {
		return stats, err
	}
	return stats, nil
}

func loadFeedIDByURL(ctx context.Context, tx *sql.Tx) (map[string]int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, url FROM feeds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int)
	for rows.Next() {
		var id int
		var u string
		if err := rows.Scan(&id, &u); err != nil {
			return nil, err
		}
		m[u] = id
	}
	return m, rows.Err()
}

func ownerOrNil(p *int) interface{} {
	if p == nil {
		return nil
	}
	return *p
}
