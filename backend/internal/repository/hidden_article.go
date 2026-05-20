package repository

import (
	"database/sql"
	"time"
)

// HiddenArticleRepository manages per-user soft-delete ("hide") state for
// articles. The articles row itself is untouched; this table is a pure
// visibility overlay applied by user-facing list queries.
type HiddenArticleRepository struct {
	db *sql.DB
}

func NewHiddenArticleRepository(db *sql.DB) *HiddenArticleRepository {
	return &HiddenArticleRepository{db: db}
}

// Hide marks the article hidden for the user. Idempotent: a second call
// returns the original hidden_at rather than refreshing it, so toast-undo
// after a delay can't accidentally extend the hidden window.
func (r *HiddenArticleRepository) Hide(userID, articleID int) (time.Time, error) {
	query := `
		INSERT INTO hidden_articles (user_id, article_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id, article_id) DO UPDATE
		  SET hidden_at = hidden_articles.hidden_at
		RETURNING hidden_at
	`
	var ts time.Time
	err := r.db.QueryRow(query, userID, articleID).Scan(&ts)
	return ts, err
}

// Unhide removes the hide row. No error when the row doesn't exist —
// undoing a non-hidden article is a no-op.
func (r *HiddenArticleRepository) Unhide(userID, articleID int) error {
	_, err := r.db.Exec(`DELETE FROM hidden_articles WHERE user_id = $1 AND article_id = $2`, userID, articleID)
	return err
}

// IsHidden returns (true, hidden_at) when the article is hidden for the user,
// (false, zero-time) otherwise.
func (r *HiddenArticleRepository) IsHidden(userID, articleID int) (bool, time.Time, error) {
	var ts time.Time
	err := r.db.QueryRow(`SELECT hidden_at FROM hidden_articles WHERE user_id = $1 AND article_id = $2`, userID, articleID).Scan(&ts)
	if err == sql.ErrNoRows {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}
	return true, ts, nil
}
