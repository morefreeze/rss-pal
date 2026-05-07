package repository

import (
	"database/sql"
	"fmt"

	"github.com/bytedance/rss-pal/internal/model"
)

type UserInsightRepository struct {
	db *sql.DB
}

func NewUserInsightRepository(db *sql.DB) *UserInsightRepository {
	return &UserInsightRepository{db: db}
}

func (r *UserInsightRepository) Insert(userID int, content, triggeredBy, model string) error {
	if triggeredBy != "auto" && triggeredBy != "manual" {
		return fmt.Errorf("invalid triggered_by: %s", triggeredBy)
	}
	_, err := r.db.Exec(`
		INSERT INTO user_insights (user_id, content, triggered_by, model)
		VALUES ($1, $2, $3, NULLIF($4, ''))
	`, userID, content, triggeredBy, model)
	return err
}

// GetLatest returns the most recent insight for a user, or nil if none.
func (r *UserInsightRepository) GetLatest(userID int) (*model.UserInsight, error) {
	row := r.db.QueryRow(`
		SELECT id, user_id, content, triggered_by, COALESCE(model, ''), generated_at
		FROM user_insights
		WHERE user_id = $1
		ORDER BY generated_at DESC
		LIMIT 1
	`, userID)
	var ui model.UserInsight
	err := row.Scan(&ui.ID, &ui.UserID, &ui.Content, &ui.TriggeredBy, &ui.Model, &ui.GeneratedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ui, nil
}

// CountManualSince returns how many manual generations the user has done within
// the given Postgres interval (e.g. "1 day", "30 days").
func (r *UserInsightRepository) CountManualSince(userID int, interval string) (int, error) {
	q := fmt.Sprintf(`
		SELECT COUNT(*) FROM user_insights
		WHERE user_id = $1 AND triggered_by = 'manual'
		  AND generated_at > NOW() - INTERVAL '%s'
	`, interval)
	var n int
	if err := r.db.QueryRow(q, userID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
