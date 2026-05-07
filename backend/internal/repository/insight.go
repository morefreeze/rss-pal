package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/lib/pq"
)

type UserInsightRepository struct {
	db *sql.DB
}

func NewUserInsightRepository(db *sql.DB) *UserInsightRepository {
	return &UserInsightRepository{db: db}
}

// ErrPendingExists is returned by InsertPending when a pending row already
// exists for the user (DB-enforced via unique partial index).
var ErrPendingExists = errors.New("pending insight already exists for user")

// InsertPending creates a new pending row and returns its id. Fails with
// ErrPendingExists if the user already has a pending row.
func (r *UserInsightRepository) InsertPending(userID int, triggeredBy, modelName string) (int, error) {
	if triggeredBy != "auto" && triggeredBy != "manual" {
		return 0, fmt.Errorf("invalid triggered_by: %s", triggeredBy)
	}
	var id int
	err := r.db.QueryRow(`
		INSERT INTO user_insights (user_id, content, status, triggered_by, model)
		VALUES ($1, NULL, 'pending', $2, NULLIF($3, ''))
		RETURNING id
	`, userID, triggeredBy, modelName).Scan(&id)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return 0, ErrPendingExists
		}
		return 0, err
	}
	return id, nil
}

// MarkDone updates a pending row to status='done' with the AI-generated content.
func (r *UserInsightRepository) MarkDone(id int, content string) error {
	res, err := r.db.Exec(`
		UPDATE user_insights
		SET content = $2, status = 'done', error_msg = NULL, generated_at = NOW()
		WHERE id = $1 AND status = 'pending'
	`, id, content)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no pending insight with id=%d", id)
	}
	return nil
}

// MarkFailed updates a pending row to status='failed' with an error message.
func (r *UserInsightRepository) MarkFailed(id int, errMsg string) error {
	if len(errMsg) > 1000 {
		errMsg = errMsg[:1000]
	}
	_, err := r.db.Exec(`
		UPDATE user_insights
		SET status = 'failed', error_msg = $2, generated_at = NOW()
		WHERE id = $1 AND status = 'pending'
	`, id, errMsg)
	return err
}

// GetLatest returns the most recent insight for a user (any status), or nil.
func (r *UserInsightRepository) GetLatest(userID int) (*model.UserInsight, error) {
	row := r.db.QueryRow(`
		SELECT id, user_id, COALESCE(content, ''), status, COALESCE(error_msg, ''),
		       triggered_by, COALESCE(model, ''), generated_at
		FROM user_insights
		WHERE user_id = $1
		ORDER BY generated_at DESC
		LIMIT 1
	`, userID)
	var ui model.UserInsight
	err := row.Scan(&ui.ID, &ui.UserID, &ui.Content, &ui.Status, &ui.ErrorMsg,
		&ui.TriggeredBy, &ui.Model, &ui.GeneratedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ui, nil
}

// CountManualSince counts only completed (status='done') manual generations
// in the given window. Pending and failed do not consume quota.
func (r *UserInsightRepository) CountManualSince(userID int, window time.Duration) (int, error) {
	var n int
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM user_insights
		WHERE user_id = $1 AND triggered_by = 'manual' AND status = 'done'
		  AND generated_at > NOW() - make_interval(secs => $2)
	`, userID, window.Seconds()).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// HasPending reports whether the user currently has a pending row.
func (r *UserInsightRepository) HasPending(userID int) (bool, error) {
	var exists bool
	err := r.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM user_insights WHERE user_id = $1 AND status = 'pending')
	`, userID).Scan(&exists)
	return exists, err
}
