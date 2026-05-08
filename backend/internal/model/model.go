package model

import "time"

type Feed struct {
	ID               int        `json:"id" db:"id"`
	URL              string     `json:"url" db:"url"`
	Title            string     `json:"title" db:"title"`
	LastFetchedAt    *time.Time `json:"last_fetched_at" db:"last_fetched_at"`
	FetchIntervalMin int        `json:"fetch_interval_minutes" db:"fetch_interval_minutes"`
	ETag             string     `json:"etag" db:"etag"`
	LastModified     string     `json:"last_modified" db:"last_modified"`
	IsActive         bool       `json:"is_active" db:"is_active"`
	OwnerID          *int       `json:"owner_id" db:"owner_id"`
	FeedType         string     `json:"feed_type" db:"feed_type"` // "rss" or "html"
	Status           string     `json:"status" db:"status"`
	PriorityWeight   float64    `json:"priority_weight" db:"priority_weight"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	ArticleCount     int        `json:"article_count" db:"article_count"`
	UnreadCount      int        `json:"unread_count" db:"unread_count"`
}

type Article struct {
	ID                   int        `json:"id" db:"id"`
	FeedID               int        `json:"feed_id" db:"feed_id"`
	FeedTitle            string     `json:"feed_title,omitempty" db:"feed_title"`
	Title                string     `json:"title" db:"title"`
	URL                  string     `json:"url" db:"url"`
	Content              string     `json:"content" db:"content"`
	PublishedAt          *time.Time `json:"published_at" db:"published_at"`
	SummaryBrief         string     `json:"summary_brief" db:"summary_brief"`
	SummaryDetailed      string     `json:"summary_detailed" db:"summary_detailed"`
	FetchedAt            time.Time  `json:"fetched_at" db:"fetched_at"`
	WordCount            int        `json:"word_count" db:"word_count"`
	ReadingMinutes       int        `json:"reading_minutes" db:"reading_minutes"`
	IsRead               bool       `json:"is_read" db:"is_read"`
	MediaURL             string     `json:"media_url,omitempty" db:"media_url"`
	MediaType            string     `json:"media_type,omitempty" db:"media_type"`
	MediaDurationSeconds int        `json:"media_duration_seconds,omitempty" db:"media_duration_seconds"`
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
	ID               int       `json:"id" db:"id"`
	Topic            string    `json:"topic" db:"topic"`
	Weight           float64   `json:"weight" db:"weight"`
	LastReinforcedAt time.Time `json:"last_reinforced_at" db:"last_reinforced_at"`
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

// InterestTag is the fine-grained counterpart of InterestTopic.
type InterestTag struct {
	ID               int       `json:"id" db:"id"`
	Tag              string    `json:"tag" db:"tag"`
	Weight           float64   `json:"weight" db:"weight"`
	LastReinforcedAt time.Time `json:"last_reinforced_at" db:"last_reinforced_at"`
}

// UserInsight is one persisted AI-generated insight (auto or manual).
type UserInsight struct {
	ID              int                       `json:"id" db:"id"`
	UserID          int                       `json:"user_id" db:"user_id"`
	Content         string                    `json:"content" db:"content"`
	Status          string                    `json:"status" db:"status"` // "pending" | "done" | "failed"
	ErrorMsg        string                    `json:"error_msg,omitempty" db:"error_msg"`
	TriggeredBy     string                    `json:"triggered_by" db:"triggered_by"` // "auto" | "manual"
	Model           string                    `json:"model,omitempty" db:"model"`
	GeneratedAt     time.Time                 `json:"generated_at" db:"generated_at"`
	Recommendations []RecommendationDirection `json:"recommendations,omitempty" db:"recommendations"`
}

// ArticleRecommendation is one (article_id, reason) entry inside a direction.
type ArticleRecommendation struct {
	ArticleID int    `json:"article_id"`
	Reason    string `json:"reason"`
}

// RecommendationDirection groups article recommendations under one interest
// direction. Kind is "core" (strengthen existing top interest) or "emerging"
// (weak signal that recurs).
type RecommendationDirection struct {
	Direction     string                  `json:"direction"`
	DirectionKind string                  `json:"direction_kind"`
	Articles      []ArticleRecommendation `json:"articles"`
}

// InsightCandidate is one row from ArticleRepository.GetInsightCandidates,
// shipped to the AI prompt as a candidate article it may select.
type InsightCandidate struct {
	Article     Article
	AlreadyRead bool   // true when from the past-favorites slice (read 30–180d ago, ever liked/saved)
	BriefShort  string // first 60 runes of summary_brief, "" if none
}

// Classification is what the AI returns for one article.
type Classification struct {
	Topic string   `json:"topic"`
	Tags  []string `json:"tags"`
}

// PlaybackProgress is the per-user resume position for an audio article.
type PlaybackProgress struct {
	ID              int       `json:"id" db:"id"`
	UserID          int       `json:"user_id" db:"user_id"`
	ArticleID       int       `json:"article_id" db:"article_id"`
	PositionSeconds int       `json:"position_seconds" db:"position_seconds"`
	LastPlayedAt    time.Time `json:"last_played_at" db:"last_played_at"`
	IsCompleted     bool      `json:"is_completed" db:"is_completed"`
}

// ArticleEvent records a behavioral signal about a user-article interaction.
// event_type ∈ {"exposure", "click", "completed_read"}.
type ArticleEvent struct {
	ID         int64     `json:"id" db:"id"`
	UserID     int       `json:"user_id" db:"user_id"`
	ArticleID  int       `json:"article_id" db:"article_id"`
	EventType  string    `json:"event_type" db:"event_type"`
	OccurredAt time.Time `json:"occurred_at" db:"occurred_at"`
}

const (
	EventTypeExposure      = "exposure"
	EventTypeClick         = "click"
	EventTypeCompletedRead = "completed_read"
)
