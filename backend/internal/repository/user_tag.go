package repository

import (
	"database/sql"
	"errors"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/lib/pq"
)

// ErrTagNameConflict is returned when a tag name already exists for the user.
var ErrTagNameConflict = errors.New("tag name already exists")

type UserTagRepository struct {
	db *sql.DB
}

func NewUserTagRepository(db *sql.DB) *UserTagRepository {
	return &UserTagRepository{db: db}
}

// GetTagsForUser returns the user's manual tags with the count of distinct
// SAVED articles each tag is currently bound to. Tags with zero saved
// articles are still returned (they may be bound to non-saved articles).
func (r *UserTagRepository) GetTagsForUser(userID int) ([]model.UserTag, error) {
	rows, err := r.db.Query(`
		SELECT t.id, t.user_id, t.name, t.created_at,
		       COUNT(DISTINCT CASE WHEN p.article_id IS NOT NULL THEN aut.article_id END) AS article_count
		FROM user_tags t
		LEFT JOIN article_user_tags aut
		       ON aut.tag_id = t.id AND aut.user_id = t.user_id
		LEFT JOIN user_preferences p
		       ON p.article_id = aut.article_id
		      AND p.user_id = t.user_id
		      AND p.signal_type = 'save'
		WHERE t.user_id = $1
		GROUP BY t.id
		ORDER BY t.name ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []model.UserTag
	for rows.Next() {
		var t model.UserTag
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.ArticleCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// CreateTag inserts (or returns existing) a tag by (user_id, name).
// Returns the existing or newly-created tag's ID.
func (r *UserTagRepository) CreateTag(userID int, name string) (int, error) {
	var id int
	err := r.db.QueryRow(`
		INSERT INTO user_tags (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, userID, name).Scan(&id)
	return id, err
}

// RenameTag changes the name. Returns ErrTagNameConflict on unique violation.
// Returns sql.ErrNoRows if the tag does not belong to the user.
func (r *UserTagRepository) RenameTag(userID, tagID int, name string) error {
	res, err := r.db.Exec(`
		UPDATE user_tags SET name = $1 WHERE id = $2 AND user_id = $3
	`, name, tagID, userID)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return ErrTagNameConflict
		}
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteTag removes a tag (cascades article_user_tags via FK).
// Returns sql.ErrNoRows if not found / not owned.
func (r *UserTagRepository) DeleteTag(userID, tagID int) error {
	res, err := r.db.Exec(`DELETE FROM user_tags WHERE id = $1 AND user_id = $2`, tagID, userID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
