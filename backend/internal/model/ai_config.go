package model

import "time"

type SummaryTemplate struct {
	ID             int       `json:"id"`
	UserID         *int      `json:"user_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Style          string    `json:"style"`
	BriefPrompt    string    `json:"brief_prompt"`
	DetailedPrompt string    `json:"detailed_prompt"`
	IsSystem       bool      `json:"is_system"`
	CreatedAt      time.Time `json:"created_at"`
}
