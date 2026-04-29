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

type UserAIConfig struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	APIKey    string    `json:"api_key"` // 返回时脱敏（只返回前4位+***）
	BaseURL   string    `json:"base_url"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ShareToken struct {
	ID        int       `json:"id"`
	ArticleID int       `json:"article_id"`
	Token     string    `json:"token"`
	CreatedBy int       `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}
