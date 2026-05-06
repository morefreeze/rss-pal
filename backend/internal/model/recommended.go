package model

import "time"

type RecommendedFeed struct {
	ID          int       `json:"id" db:"id"`
	URL         string    `json:"url" db:"url"`
	Title       string    `json:"title" db:"title"`
	Description string    `json:"description" db:"description"`
	Category    string    `json:"category" db:"category"`
	Language    string    `json:"language" db:"language"`
	FeedType    string    `json:"feed_type" db:"feed_type"`
	IsBroken    bool      `json:"is_broken" db:"is_broken"`
	SortOrder   int       `json:"sort_order" db:"sort_order"`
	Subscribed  bool      `json:"subscribed"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}
