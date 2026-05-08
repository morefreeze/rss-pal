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

type ArticleUserTagRepository struct {
	db *sql.DB
}

func NewArticleUserTagRepository(db *sql.DB) *ArticleUserTagRepository {
	return &ArticleUserTagRepository{db: db}
}

// BindByName ensures (article_id, tag with given name, user) is bound.
// Creates the tag in the user's dictionary if it does not exist.
// Idempotent: returns the tag ID whether new or pre-existing.
func (r *ArticleUserTagRepository) BindByName(articleID, userID int, name string) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var tagID int
	err = tx.QueryRow(`
		INSERT INTO user_tags (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, userID, name).Scan(&tagID)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(`
		INSERT INTO article_user_tags (article_id, tag_id, user_id) VALUES ($1, $2, $3)
		ON CONFLICT (article_id, tag_id) DO NOTHING
	`, articleID, tagID, userID)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return tagID, nil
}

// Unbind removes the binding. Returns sql.ErrNoRows if not bound.
func (r *ArticleUserTagRepository) Unbind(articleID, tagID, userID int) error {
	res, err := r.db.Exec(`
		DELETE FROM article_user_tags
		WHERE article_id = $1 AND tag_id = $2 AND user_id = $3
	`, articleID, tagID, userID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetSourceForArticle returns the feed-derived source tag (id + title).
func (r *ArticleUserTagRepository) GetSourceForArticle(articleID int) (model.ArticleTagSource, error) {
	var s model.ArticleTagSource
	err := r.db.QueryRow(`
		SELECT f.id, f.title
		FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		WHERE a.id = $1
	`, articleID).Scan(&s.FeedID, &s.Title)
	return s, err
}

// GetManualForArticle returns the user's manual tags bound to the article.
func (r *ArticleUserTagRepository) GetManualForArticle(articleID, userID int) ([]model.UserTag, error) {
	rows, err := r.db.Query(`
		SELECT t.id, t.user_id, t.name, t.created_at
		FROM user_tags t
		JOIN article_user_tags aut ON aut.tag_id = t.id AND aut.user_id = t.user_id
		WHERE aut.article_id = $1 AND t.user_id = $2
		ORDER BY t.name ASC
	`, articleID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []model.UserTag
	for rows.Next() {
		var t model.UserTag
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// GetManualForArticles batch version — returns map[articleID][]UserTag.
// Used by /api/saved to attach tags to article cards.
func (r *ArticleUserTagRepository) GetManualForArticles(articleIDs []int, userID int) (map[int][]model.UserTag, error) {
	out := map[int][]model.UserTag{}
	if len(articleIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(`
		SELECT aut.article_id, t.id, t.user_id, t.name, t.created_at
		FROM user_tags t
		JOIN article_user_tags aut ON aut.tag_id = t.id AND aut.user_id = t.user_id
		WHERE aut.article_id = ANY($1::int[]) AND t.user_id = $2
		ORDER BY aut.article_id, t.name ASC
	`, pq.Array(articleIDs), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var aid int
		var t model.UserTag
		if err := rows.Scan(&aid, &t.ID, &t.UserID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		out[aid] = append(out[aid], t)
	}
	return out, rows.Err()
}
