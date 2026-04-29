package repository

import (
	"database/sql"
)

type StatsRepository struct {
	db *sql.DB
}

func NewStatsRepository(db *sql.DB) *StatsRepository {
	return &StatsRepository{db: db}
}

type FeedStats struct {
	TotalFeeds      int `json:"total_feeds"`
	ActiveFeeds     int `json:"active_feeds"`
	TotalArticles   int `json:"total_articles"`
	TodayArticles   int `json:"today_articles"`
	WithContent     int `json:"with_content"`
	WithoutContent  int `json:"without_content"`
	WithSummary     int `json:"with_summary"`
}

type FetchProgress struct {
	FeedID          int    `json:"feed_id"`
	FeedTitle       string `json:"feed_title"`
	FeedURL         string `json:"feed_url"`
	LastFetchedAt   *string `json:"last_fetched_at"`
	ArticleCount    int    `json:"article_count"`
	ContentProgress int    `json:"content_progress"` // percentage
	SummaryProgress int    `json:"summary_progress"` // percentage
}

func (r *StatsRepository) GetStats(userID int) (*FeedStats, error) {
	stats := &FeedStats{}

	// Total feeds visible to user (shared + own)
	r.db.QueryRow(`SELECT COUNT(*) FROM feeds WHERE owner_id IS NULL OR owner_id = $1`, userID).Scan(&stats.TotalFeeds)

	// Active feeds visible to user
	r.db.QueryRow(`SELECT COUNT(*) FROM feeds WHERE is_active = true AND (owner_id IS NULL OR owner_id = $1)`, userID).Scan(&stats.ActiveFeeds)

	// Total articles from feeds visible to user
	r.db.QueryRow(`SELECT COUNT(*) FROM articles a JOIN feeds f ON a.feed_id = f.id WHERE f.owner_id IS NULL OR f.owner_id = $1`, userID).Scan(&stats.TotalArticles)

	// Today's articles from feeds visible to user
	r.db.QueryRow(`SELECT COUNT(*) FROM articles a JOIN feeds f ON a.feed_id = f.id WHERE (f.owner_id IS NULL OR f.owner_id = $1) AND a.fetched_at > NOW() - INTERVAL '24 hours'`, userID).Scan(&stats.TodayArticles)

	// Articles with content (> 200 chars) from feeds visible to user
	r.db.QueryRow(`SELECT COUNT(*) FROM articles a JOIN feeds f ON a.feed_id = f.id WHERE (f.owner_id IS NULL OR f.owner_id = $1) AND LENGTH(a.content) > 200`, userID).Scan(&stats.WithContent)

	// Articles without content from feeds visible to user
	r.db.QueryRow(`SELECT COUNT(*) FROM articles a JOIN feeds f ON a.feed_id = f.id WHERE (f.owner_id IS NULL OR f.owner_id = $1) AND (LENGTH(a.content) <= 200 OR a.content IS NULL)`, userID).Scan(&stats.WithoutContent)

	// Articles with summary from feeds visible to user
	r.db.QueryRow(`SELECT COUNT(*) FROM articles a JOIN feeds f ON a.feed_id = f.id WHERE (f.owner_id IS NULL OR f.owner_id = $1) AND a.summary_brief IS NOT NULL AND a.summary_brief != ''`, userID).Scan(&stats.WithSummary)

	return stats, nil
}

func (r *StatsRepository) GetFetchProgress(userID int) ([]FetchProgress, error) {
	query := `
		SELECT
			f.id,
			COALESCE(f.title, f.url),
			f.url,
			f.last_fetched_at,
			COUNT(a.id) as article_count,
			COALESCE(ROUND(100.0 * SUM(CASE WHEN LENGTH(a.content) > 200 THEN 1 ELSE 0 END) / NULLIF(COUNT(a.id), 0)), 0) as content_progress,
			COALESCE(ROUND(100.0 * SUM(CASE WHEN a.summary_brief IS NOT NULL AND a.summary_brief != '' THEN 1 ELSE 0 END) / NULLIF(COUNT(a.id), 0)), 0) as summary_progress
		FROM feeds f
		LEFT JOIN articles a ON f.id = a.feed_id
		WHERE f.is_active = true AND (f.owner_id IS NULL OR f.owner_id = $1)
		GROUP BY f.id, f.title, f.url, f.last_fetched_at
		ORDER BY f.last_fetched_at DESC NULLS LAST
	`

	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var progress []FetchProgress
	for rows.Next() {
		var p FetchProgress
		var lastFetched sql.NullString
		err := rows.Scan(&p.FeedID, &p.FeedTitle, &p.FeedURL, &lastFetched, &p.ArticleCount, &p.ContentProgress, &p.SummaryProgress)
		if err != nil {
			return nil, err
		}
		if lastFetched.Valid {
			p.LastFetchedAt = &lastFetched.String
		}
		progress = append(progress, p)
	}

	return progress, nil
}
