package repository

import (
	"database/sql"

	"github.com/bytedance/rss-pal/internal/model"
)

type RecommendedFeedRepository struct {
	db *sql.DB
}

func NewRecommendedFeedRepository(db *sql.DB) *RecommendedFeedRepository {
	return &RecommendedFeedRepository{db: db}
}

// ListWithSubscriptionStatus returns the catalog with `subscribed = true` when
// the URL already exists in `feeds` (regardless of owner), so the UI can show
// a "✓ 已订阅" badge for shared seeds and the user's own feeds alike.
func (r *RecommendedFeedRepository) ListWithSubscriptionStatus(userID int) ([]model.RecommendedFeed, error) {
	rows, err := r.db.Query(`
		SELECT rf.id, rf.url, rf.title, rf.description, rf.category, rf.language, rf.feed_type, rf.is_broken, rf.sort_order, rf.created_at,
		       (f.id IS NOT NULL) AS subscribed
		FROM recommended_feeds rf
		LEFT JOIN feeds f ON f.url = rf.url AND (f.owner_id IS NULL OR f.owner_id = $1)
		ORDER BY rf.category, rf.sort_order, rf.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.RecommendedFeed, 0)
	for rows.Next() {
		var rf model.RecommendedFeed
		var description sql.NullString
		if err := rows.Scan(&rf.ID, &rf.URL, &rf.Title, &description, &rf.Category, &rf.Language, &rf.FeedType, &rf.IsBroken, &rf.SortOrder, &rf.CreatedAt, &rf.Subscribed); err != nil {
			return nil, err
		}
		rf.Description = description.String
		out = append(out, rf)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RecommendedFeedRepository) GetByID(id int) (*model.RecommendedFeed, error) {
	var rf model.RecommendedFeed
	var description sql.NullString
	err := r.db.QueryRow(`
		SELECT id, url, title, description, category, language, feed_type, is_broken, sort_order, created_at
		FROM recommended_feeds WHERE id = $1
	`, id).Scan(&rf.ID, &rf.URL, &rf.Title, &description, &rf.Category, &rf.Language, &rf.FeedType, &rf.IsBroken, &rf.SortOrder, &rf.CreatedAt)
	if err != nil {
		return nil, err
	}
	rf.Description = description.String
	return &rf, nil
}
