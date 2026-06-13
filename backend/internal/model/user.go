package model

import "time"

type User struct {
	ID                 int       `json:"id"`
	Username           string    `json:"username"`
	PasswordHash       string    `json:"-"`
	IsAdmin            bool      `json:"is_admin"`
	CreatedAt          time.Time `json:"created_at"`
	SharedVisibleFrom  time.Time `json:"shared_visible_from"`
}

type InviteCode struct {
	ID        int        `json:"id"`
	Code      string     `json:"code"`
	CreatedBy int        `json:"created_by"`
	UsedBy    *int       `json:"used_by"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

type RegisterRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
	Code     string `json:"code" binding:"required"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	// Remember asks the server to issue a long-lived refresh token so the
	// client can re-mint access JWTs without re-prompting the user.
	Remember bool `json:"remember"`
}

type CreateInviteCodeRequest struct {
	ExpiresInHours int `json:"expires_in_hours"`
}
