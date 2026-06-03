package repository

import (
	"database/sql"
	"errors"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
)

type EventRepository struct {
	db Querier
}

func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

// WithCtx returns a repository view bound to the per-request transaction
// stashed under ctxkey.Tx by RLSTxMiddleware. Falls back to the underlying
// handle if no tx is present.
func (r *EventRepository) WithCtx(c ctxkey.CtxGetter) *EventRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &EventRepository{db: q}
		}
	}
	return r
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
