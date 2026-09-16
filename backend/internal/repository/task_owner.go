package repository

import (
	"context"

	"github.com/bytedance/rss-pal/internal/taskbudget"
)

// TaskContext resolves ownership from stored data, never user-supplied IDs.
// Worker bypass access is intentional; failure must stop work, not charge system.
func (r *ArticleRepository) TaskContext(ctx context.Context, articleID int) (context.Context, error) {
	var owner int
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(f.owner_id,0) FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.id=$1`, articleID).Scan(&owner)
	if err != nil {
		return nil, err
	}
	return taskbudget.WithOwner(ctx, owner), nil
}
