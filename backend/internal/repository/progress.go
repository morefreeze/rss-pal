package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

type ProgressRepository struct {
	db *sql.DB
}

func NewProgressRepository(db *sql.DB) *ProgressRepository {
	return &ProgressRepository{db: db}
}

func (r *ProgressRepository) GetByArticleAndUser(articleID, userID int) (*model.ReadingProgress, error) {
	query := `SELECT id, user_id, article_id, scroll_position, last_read_at, is_completed FROM reading_progress WHERE article_id = $1 AND user_id = $2`
	var p model.ReadingProgress
	err := r.db.QueryRow(query, articleID, userID).Scan(&p.ID, &p.UserID, &p.ArticleID, &p.ScrollPosition, &p.LastReadAt, &p.IsCompleted)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ProgressRepository) Upsert(progress *model.ReadingProgress) error {
	query := `
		INSERT INTO reading_progress (user_id, article_id, scroll_position, last_read_at, is_completed)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (article_id, user_id) DO UPDATE SET
			scroll_position = GREATEST(reading_progress.scroll_position, EXCLUDED.scroll_position),
			last_read_at = EXCLUDED.last_read_at,
			is_completed = reading_progress.is_completed OR EXCLUDED.is_completed
		RETURNING id, scroll_position, is_completed
	`
	return r.db.QueryRow(query, progress.UserID, progress.ArticleID, progress.ScrollPosition, progress.LastReadAt, progress.IsCompleted).Scan(&progress.ID, &progress.ScrollPosition, &progress.IsCompleted)
}

func (r *ProgressRepository) Reset(articleID, userID int) error {
	query := `
		UPDATE reading_progress
		SET scroll_position = 0, last_read_at = NOW(), is_completed = false
		WHERE article_id = $1 AND user_id = $2
	`
	_, err := r.db.Exec(query, articleID, userID)
	return err
}

func (r *ProgressRepository) UpdateTimestamp(articleID int, t time.Time) error {
	query := `UPDATE reading_progress SET last_read_at = $1 WHERE article_id = $2`
	_, err := r.db.Exec(query, t, articleID)
	return err
}

// MarkAllRead marks every article visible under the given filters as read.
// Filters mirror ArticleRepository.GetAll so the affected set matches what
// the user currently sees in the list. Pass feedID=nil / unreadOnly=false /
// savedOnly=false to apply across the whole library.
func (r *ProgressRepository) MarkAllRead(userID int, feedID *int, unreadOnly, savedOnly bool) error {
	args := []interface{}{userID}
	argIdx := 2
	joins := ""
	conditions := []string{"(f.owner_id IS NULL OR f.owner_id = $1)"}

	if feedID != nil {
		conditions = append(conditions, fmt.Sprintf("a.feed_id = $%d", argIdx))
		args = append(args, *feedID)
		argIdx++
	}
	if unreadOnly {
		joins += " LEFT JOIN reading_progress rp ON rp.article_id = a.id AND rp.user_id = $1"
		conditions = append(conditions, "COALESCE(rp.is_completed, false) = false")
	}
	if savedOnly {
		joins += " LEFT JOIN user_preferences up_save ON up_save.article_id = a.id AND up_save.user_id = $1 AND up_save.signal_type = 'save'"
		conditions = append(conditions, fmt.Sprintf("up_save.signal_value = $%d", argIdx))
		args = append(args, 1.0)
		argIdx++
	}

	where := conditions[0]
	for i := 1; i < len(conditions); i++ {
		where += " AND " + conditions[i]
	}

	query := `
		INSERT INTO reading_progress (user_id, article_id, scroll_position, last_read_at, is_completed)
		SELECT $1, a.id, 1.0, NOW(), true
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id` + joins + `
		WHERE ` + where + `
		ON CONFLICT (article_id, user_id) DO UPDATE SET
			is_completed = true, scroll_position = 1.0, last_read_at = NOW()
	`
	_, err := r.db.Exec(query, args...)
	return err
}
