package repository

import (
	"database/sql"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/bytedance/rss-pal/internal/service"
)

type FeedHealthRepository struct {
	db Querier
}

func NewFeedHealthRepository(db *sql.DB) *FeedHealthRepository {
	return &FeedHealthRepository{db: db}
}

// WithCtx returns a repository view bound to the per-request transaction
// stashed under ctxkey.Tx by RLSTxMiddleware. Falls back to the underlying
// handle if no tx is present.
func (r *FeedHealthRepository) WithCtx(c ctxkey.CtxGetter) *FeedHealthRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &FeedHealthRepository{db: q}
		}
	}
	return r
}

// ComputeMetrics returns one FeedMetrics per non-archived feed visible to the user.
// `window` is the user-selected window (30d or 90d) for display columns;
// 30d/90d-specific counts (used by pruning rules) are also returned.
//
// Implementation: a single query with three windowed aggregations.
// Archived feeds are excluded; paused feeds are included so user can see why
// they paused them.
func (r *FeedHealthRepository) ComputeMetrics(userID int, window time.Duration) ([]service.FeedMetrics, error) {
	windowSeconds := int(window.Seconds())

	query := `
WITH events AS (
    SELECT article_id, event_type, occurred_at, user_id
    FROM article_events
    WHERE user_id = $1
),
articles_w AS (
    SELECT id, feed_id, fetched_at FROM articles
    WHERE fetched_at >= NOW() - ($2 || ' seconds')::INTERVAL
),
articles_30d AS (
    SELECT id, feed_id FROM articles WHERE fetched_at >= NOW() - INTERVAL '30 days'
),
articles_90d AS (
    SELECT id, feed_id FROM articles WHERE fetched_at >= NOW() - INTERVAL '90 days'
),
prefs_w AS (
    SELECT article_id, signal_type, signal_value FROM user_preferences
    WHERE user_id = $1 AND created_at >= NOW() - ($2 || ' seconds')::INTERVAL
),
read_dur AS (
    SELECT a.feed_id, p.signal_value
    FROM user_preferences p
    JOIN articles a ON a.id = p.article_id
    WHERE p.user_id = $1
      AND p.signal_type = 'read_duration'
      AND p.created_at >= NOW() - ($2 || ' seconds')::INTERVAL
)
SELECT
    f.id,
    COALESCE(f.title, f.url) AS feed_title,
    f.last_fetched_at,
    -- window-bound counts
    (SELECT COUNT(*) FROM articles_w aw WHERE aw.feed_id = f.id) AS produced,
    (SELECT COUNT(DISTINCT e.article_id) FROM events e
       JOIN articles_w aw ON aw.id = e.article_id
       WHERE e.event_type = 'exposure'
         AND aw.feed_id = f.id) AS exposures,
    (SELECT COUNT(DISTINCT e.article_id) FROM events e
       JOIN articles_w aw ON aw.id = e.article_id
       WHERE e.event_type = 'click'
         AND aw.feed_id = f.id) AS clicks_w,
    (SELECT COUNT(DISTINCT e.article_id) FROM events e
       JOIN articles_w aw ON aw.id = e.article_id
       WHERE e.event_type = 'completed_read'
         AND aw.feed_id = f.id) AS completed_w,
    COALESCE((SELECT (PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY signal_value)) / 60.0
       FROM read_dur rd WHERE rd.feed_id = f.id), 0) AS avg_duration_min,
    COALESCE((SELECT
        SUM(CASE signal_type WHEN 'like' THEN 1 WHEN 'save' THEN 1 WHEN 'dislike' THEN -1 ELSE 0 END)::FLOAT
        FROM prefs_w p JOIN articles a ON a.id = p.article_id
        WHERE a.feed_id = f.id), 0) AS feedback_density,
    (SELECT MAX(occurred_at) FROM events e
       JOIN articles a ON a.id = e.article_id
       WHERE e.event_type IN ('click','completed_read')
         AND a.feed_id = f.id) AS last_active_at,
    -- 30d
    (SELECT COUNT(*) FROM articles_30d a30 WHERE a30.feed_id = f.id) AS produced_30d,
    (SELECT COUNT(DISTINCT e.article_id) FROM events e
       JOIN articles a ON a.id = e.article_id
       WHERE e.event_type = 'click'
         AND e.occurred_at >= NOW() - INTERVAL '30 days'
         AND a.feed_id = f.id) AS clicks_30d,
    -- 90d
    (SELECT COUNT(*) FROM articles_90d a90 WHERE a90.feed_id = f.id) AS produced_90d,
    (SELECT COUNT(DISTINCT e.article_id) FROM events e
       JOIN articles a ON a.id = e.article_id
       WHERE e.event_type = 'click'
         AND e.occurred_at >= NOW() - INTERVAL '90 days'
         AND a.feed_id = f.id) AS clicks_90d
FROM feeds f
WHERE f.status != 'archived'
  AND (f.owner_id IS NULL OR f.owner_id = $1)
ORDER BY f.id
	`

	rows, err := r.db.Query(query, userID, windowSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []service.FeedMetrics
	for rows.Next() {
		var m service.FeedMetrics
		var lastFetched, lastActive sql.NullTime
		err := rows.Scan(
			&m.FeedID, &m.FeedTitle, &lastFetched,
			&m.Produced, &m.Exposures, &m.Clicks, &m.CompletedReads,
			&m.AvgDurationMin, &m.FeedbackDensity, &lastActive,
			&m.ProducedLast30d, &m.ClicksLast30d,
			&m.ProducedLast90d, &m.ClicksLast90d,
		)
		if err != nil {
			return nil, err
		}
		if lastFetched.Valid {
			t := lastFetched.Time
			m.LastFetchedAt = &t
		}
		if lastActive.Valid {
			t := lastActive.Time
			m.LastActiveAt = &t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
