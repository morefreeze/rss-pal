package backup

import (
	"context"
	"database/sql"
	"fmt"
)

// RestoreStats summarizes what changed on disk during a Restore call.
// Skipped counts rows whose target table cannot be safely restored because it
// references articles (which are not part of the backup).
type RestoreStats struct {
	Feeds              int `json:"feeds"`
	UserTags           int `json:"user_tags"`
	InterestCategories int `json:"interest_categories"`
	InterestTopics     int `json:"interest_topics"`
	SkippedArticleLink int `json:"skipped_article_link"`
}

// Restore applies a snapshot to the database. Restore is idempotent: each row
// is upserted by its natural key, so re-running a restore over an already-
// restored DB is a no-op on already-present rows.
//
// Article-linked tables (article_user_tags, user_preferences) are skipped:
// they reference articles by ID, and after a wipe the article rows will be
// re-fetched with fresh IDs. The counts are reported in SkippedArticleLink
// so the caller can show "X rows were not restored" in the UI.
func Restore(ctx context.Context, db *sql.DB, s *Snapshot) (RestoreStats, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RestoreStats{}, err
	}
	defer tx.Rollback()

	stats := RestoreStats{
		SkippedArticleLink: len(s.ArticleUserTags) + len(s.UserPreferences),
	}

	for i := range s.Feeds {
		f := &s.Feeds[i]
		// Upsert by URL. Owner/title/status/weight are updated so a backup
		// reflects the latest manual edits the user made before the wipe.
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
			INSERT INTO interest_topics (topic, weight, last_reinforced_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (topic) DO UPDATE SET
				weight = EXCLUDED.weight,
				last_reinforced_at = EXCLUDED.last_reinforced_at`,
			t.Topic, t.Weight, t.LastReinforcedAt)
		if err != nil {
			return stats, fmt.Errorf("upsert interest_topic %s: %w", t.Topic, err)
		}
		stats.InterestTopics++
	}

	if err := tx.Commit(); err != nil {
		return stats, err
	}
	return stats, nil
}

func ownerOrNil(p *int) interface{} {
	if p == nil {
		return nil
	}
	return *p
}
