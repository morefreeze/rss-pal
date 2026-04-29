package model

import "time"

type Feed struct {
	ID                int        `json:"id" db:"id"`
	URL               string     `json:"url" db:"url"`
	Title             string     `json:"title" db:"title"`
	LastFetchedAt     *time.Time `json:"last_fetched_at" db:"last_fetched_at"`
	FetchIntervalMin  int        `json:"fetch_interval_minutes" db:"fetch_interval_minutes"`
	ETag              string     `json:"etag" db:"etag"`
	LastModified      string     `json:"last_modified" db:"last_modified"`
	IsActive          bool       `json:"is_active" db:"is_active"`
	OwnerID           *int       `json:"owner_id" db:"owner_id"`
	FeedType          string     `json:"feed_type" db:"feed_type"` // "rss" or "html"
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
}

type Article struct {
	ID              int        `json:"id" db:"id"`
	FeedID          int        `json:"feed_id" db:"feed_id"`
	FeedTitle       string     `json:"feed_title,omitempty" db:"feed_title"`
	Title           string     `json:"title" db:"title"`
	URL             string     `json:"url" db:"url"`
	Content         string     `json:"content" db:"content"`
	PublishedAt     *time.Time `json:"published_at" db:"published_at"`
	SummaryBrief    string     `json:"summary_brief" db:"summary_brief"`
	SummaryDetailed string     `json:"summary_detailed" db:"summary_detailed"`
	FetchedAt       time.Time  `json:"fetched_at" db:"fetched_at"`
	IsRead          bool       `json:"is_read" db:"is_read"`
}

type UserPreference struct {
	ID          int       `json:"id" db:"id"`
	UserID      int       `json:"user_id" db:"user_id"`
	ArticleID   int       `json:"article_id" db:"article_id"`
	SignalType  string    `json:"signal_type" db:"signal_type"`
	SignalValue float64   `json:"signal_value" db:"signal_value"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

type InterestTopic struct {
	ID                 int       `json:"id" db:"id"`
	Topic              string    `json:"topic" db:"topic"`
	Weight             float64   `json:"weight" db:"weight"`
	LastReinforcedAt   time.Time `json:"last_reinforced_at" db:"last_reinforced_at"`
}

type ReadingProgress struct {
	ID             int       `json:"id" db:"id"`
	UserID         int       `json:"user_id" db:"user_id"`
	ArticleID      int       `json:"article_id" db:"article_id"`
	ScrollPosition float64   `json:"scroll_position" db:"scroll_position"`
	LastReadAt     time.Time `json:"last_read_at" db:"last_read_at"`
	IsCompleted    bool      `json:"is_completed" db:"is_completed"`
}

// Request/Response types

type AddFeedRequest struct {
	URL      string `json:"url"`
	FeedType string `json:"feed_type"` // "rss" or "html", defaults to "rss"
}

type UpdateProgressRequest struct {
	ScrollPosition float64 `json:"scroll_position"`
	IsCompleted    bool    `json:"is_completed"`
}

type PreferenceRequest struct {
	ArticleID int `json:"article_id"`
}
