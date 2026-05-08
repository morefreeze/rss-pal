package repository

import (
	"database/sql"
	"errors"

	"github.com/bytedance/rss-pal/internal/model"
)

type EventRepository struct {
	db *sql.DB
}

func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

// validEventTypes mirrors the model constants for input validation at the boundary.
var validEventTypes = map[string]bool{
	model.EventTypeExposure:      true,
	model.EventTypeClick:         true,
	model.EventTypeCompletedRead: true,
}

// Insert adds one event row. Caller must validate event_type via IsValidEventType
// before calling — Insert assumes a valid type and lets the DB enforce FKs.
func (r *EventRepository) Insert(userID, articleID int, eventType string) error {
	if !validEventTypes[eventType] {
		return errors.New("invalid event type")
	}
	_, err := r.db.Exec(
		`INSERT INTO article_events (user_id, article_id, event_type) VALUES ($1, $2, $3)`,
		userID, articleID, eventType,
	)
	return err
}

// IsValidEventType exposes the validation map for handlers to early-reject bad input.
func IsValidEventType(t string) bool {
	return validEventTypes[t]
}
