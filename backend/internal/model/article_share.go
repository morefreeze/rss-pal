package model

import "time"

// ArticleShareSnapshot is the immutable, wire-safe article representation
// captured when a share is created.
type ArticleShareSnapshot struct {
	Title                string            `json:"title"`
	URL                  string            `json:"url"`
	FeedTitle            string            `json:"feed_title,omitempty"`
	PublishedAt          *time.Time        `json:"published_at,omitempty"`
	WordCount            int               `json:"word_count,omitempty"`
	ReadingMinutes       int               `json:"reading_minutes,omitempty"`
	SummaryBrief         string            `json:"summary_brief,omitempty"`
	SummaryDetailed      string            `json:"summary_detailed,omitempty"`
	Content              string            `json:"content"`
	MediaURL             string            `json:"media_url,omitempty"`
	MediaType            string            `json:"media_type,omitempty"`
	MediaDurationSeconds int               `json:"media_duration_seconds,omitempty"`
	ImageDimensions      map[string][2]int `json:"image_dimensions,omitempty"`
	SnapshottedAt        time.Time         `json:"snapshotted_at"`
}

type ArticleShare struct {
	PublicID          string
	ArticleID         int
	CreatedBy         int
	SnapshotVersion   int
	Snapshot          ArticleShareSnapshot
	ExpiresAt         *time.Time
	RevokedAt         *time.Time
	LegacyTokenDigest *string
	CreatedAt         time.Time
}
