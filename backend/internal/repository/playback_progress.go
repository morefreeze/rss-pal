package repository

import (
	"database/sql"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
)

type PlaybackProgressRepository struct {
	db Querier
}

func NewPlaybackProgressRepository(db *sql.DB) *PlaybackProgressRepository {
	return &PlaybackProgressRepository{db: db}
}

// WithCtx returns a repository view bound to the per-request transaction
// stashed under ctxkey.Tx by RLSTxMiddleware. Falls back to the underlying
// handle if no tx is present.
func (r *PlaybackProgressRepository) WithCtx(c ctxkey.CtxGetter) *PlaybackProgressRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &PlaybackProgressRepository{db: q}
		}
	}
	return r
}

// Get returns the current progress row for (user, article), or nil if absent.
func (r *PlaybackProgressRepository) Get(userID, articleID int) (*model.PlaybackProgress, error) {
	query := `
		SELECT id, user_id, article_id, position_seconds, last_played_at, is_completed
		FROM playback_progress
		WHERE user_id = $1 AND article_id = $2
	`
	var p model.PlaybackProgress
	err := r.db.QueryRow(query, userID, articleID).Scan(&p.ID, &p.UserID, &p.ArticleID, &p.PositionSeconds, &p.LastPlayedAt, &p.IsCompleted)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertResult tells the caller whether is_completed flipped from false→true on this call.
type UpsertResult struct {
	NewlyCompleted bool
}

// Upsert writes the latest position. Returns NewlyCompleted=true exactly once
// (on the call that flips is_completed false→true), so the handler knows when
// to record the completed_listen signal.
func (r *PlaybackProgressRepository) Upsert(userID, articleID, positionSeconds int, isCompleted bool) (UpsertResult, error) {
	prev, err := r.Get(userID, articleID)
	if err != nil {
		return UpsertResult{}, err
	}
	wasCompleted := prev != nil && prev.IsCompleted

	_, err = r.db.Exec(`
		INSERT INTO playback_progress (user_id, article_id, position_seconds, last_played_at, is_completed)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, article_id) DO UPDATE SET
			position_seconds = EXCLUDED.position_seconds,
			last_played_at = EXCLUDED.last_played_at,
			is_completed = playback_progress.is_completed OR EXCLUDED.is_completed
	`, userID, articleID, positionSeconds, time.Now(), isCompleted)
	if err != nil {
		return UpsertResult{}, err
	}

	return UpsertResult{NewlyCompleted: !wasCompleted && isCompleted}, nil
}
