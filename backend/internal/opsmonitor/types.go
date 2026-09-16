package opsmonitor

import "time"

type Registration struct {
	Attempts int `json:"attempts"`
	Success  int `json:"success"`
	Failed   int `json:"failed"`
}
type Captcha struct {
	Success     int `json:"success"`
	Rejected    int `json:"rejected"`
	Unavailable int `json:"unavailable"`
}
type Point struct {
	At                  time.Time `json:"at"`
	RegistrationSuccess int       `json:"registration_success"`
	RegistrationFailed  int       `json:"registration_failed"`
	CaptchaRejected     int       `json:"captcha_rejected"`
	CaptchaUnavailable  int       `json:"captcha_unavailable"`
	LimitDenied         int       `json:"limit_denied"`
}
type Group struct {
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
	TaskType string `json:"task_type"`
	UserID   int    `json:"user_id"`
	Count    int    `json:"count"`
}
type Event struct {
	ID       int64      `json:"id"`
	At       time.Time  `json:"at"`
	Kind     string     `json:"kind"`
	Reason   string     `json:"reason"`
	TaskType string     `json:"task_type"`
	UserID   int        `json:"user_id"`
	Count    int        `json:"count"`
	RetryAt  *time.Time `json:"retry_at,omitempty"`
}
type Queue struct {
	Name              string    `json:"name"`
	Status            string    `json:"status"`
	Waiting           int       `json:"waiting"`
	Running           *int      `json:"running"`
	Failed            *int      `json:"failed"`
	Expired           *int      `json:"expired"`
	OldestWaitSeconds float64   `json:"oldest_wait_seconds"`
	SnapshotAt        time.Time `json:"snapshot_at"`
	Note              string    `json:"note"`
}
type TaskCost struct {
	TaskType         string   `json:"task_type"`
	Attempts         int      `json:"attempts"`
	TodayAttempts    int      `json:"today_attempts"`
	GlobalDailyLimit int      `json:"global_daily_limit"`
	UsageRatio       float64  `json:"usage_ratio"`
	UnitPrice        *float64 `json:"unit_price"`
	EstimatedCost    *float64 `json:"estimated_cost"`
}
type UserCost struct {
	UserID   int    `json:"user_id"`
	TaskType string `json:"task_type"`
	Attempts int    `json:"attempts"`
}
type Cost struct {
	UTCDay              string     `json:"utc_day"`
	LedgerRetentionNote string     `json:"ledger_retention_note"`
	EstimateStatus      string     `json:"estimate_status"`
	EstimatedToday      *float64   `json:"estimated_today"`
	DailyAlertBudget    *float64   `json:"daily_alert_budget"`
	Currency            string     `json:"currency"`
	ByTask              []TaskCost `json:"by_task"`
	ByUser              []UserCost `json:"by_user"`
}
type Alert struct {
	Code      string  `json:"code"`
	Severity  string  `json:"severity"`
	Message   string  `json:"message"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
}
type Response struct {
	GeneratedAt              time.Time    `json:"generated_at"`
	CollectionAvailableSince time.Time    `json:"collection_available_since"`
	WindowStart              time.Time    `json:"window_start"`
	WindowEnd                time.Time    `json:"window_end"`
	Hours                    int          `json:"hours"`
	Status                   string       `json:"status"`
	RetentionDays            int          `json:"retention_days"`
	Registration             Registration `json:"registration"`
	Captcha                  Captcha      `json:"captcha"`
	LimitTotal               int          `json:"limit_total"`
	TimeSeries               []Point      `json:"time_series"`
	Groups                   []Group      `json:"groups"`
	Queues                   []Queue      `json:"queues"`
	Cost                     Cost         `json:"cost"`
	Alerts                   []Alert      `json:"alerts"`
	RecentEvents             []Event      `json:"recent_events"`
	NextBeforeID             *int64       `json:"next_before_id"`
	CaptchaExpiresAt         *time.Time   `json:"captcha_expires_at"`
	CollectionDropped        int64        `json:"collection_dropped"`
}
