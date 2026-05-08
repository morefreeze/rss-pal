package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

type FeedRepository struct {
	db *sql.DB
}

func NewFeedRepository(db *sql.DB) *FeedRepository {
	return &FeedRepository{db: db}
}

func (r *FeedRepository) scanFeed(row *sql.Row) (*model.Feed, error) {
	var f model.Feed
	var title, etag, lastModified, feedType, status sql.NullString
	var ownerID sql.NullInt64
	err := row.Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &ownerID, &feedType, &status, &f.PriorityWeight, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	f.Title = title.String
	f.ETag = etag.String
	f.LastModified = lastModified.String
	f.FeedType = feedType.String
	if f.FeedType == "" {
		f.FeedType = "rss"
	}
	f.Status = status.String
	if f.Status == "" {
		f.Status = "active"
	}
	if ownerID.Valid {
		oid := int(ownerID.Int64)
		f.OwnerID = &oid
	}
	return &f, nil
}

func (r *FeedRepository) scanFeeds(rows *sql.Rows) ([]model.Feed, error) {
	var feeds []model.Feed
	for rows.Next() {
		var f model.Feed
		var title, etag, lastModified, feedType, status sql.NullString
		var ownerID sql.NullInt64
		err := rows.Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &ownerID, &feedType, &status, &f.PriorityWeight, &f.CreatedAt)
		if err != nil {
			return nil, err
		}
		f.Title = title.String
		f.ETag = etag.String
		f.LastModified = lastModified.String
		f.FeedType = feedType.String
		if f.FeedType == "" {
			f.FeedType = "rss"
		}
		f.Status = status.String
		if f.Status == "" {
			f.Status = "active"
		}
		if ownerID.Valid {
			oid := int(ownerID.Int64)
			f.OwnerID = &oid
		}
		feeds = append(feeds, f)
	}
	return feeds, nil
}

func (r *FeedRepository) GetAll() ([]model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at FROM feeds ORDER BY created_at DESC`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanFeeds(rows)
}

func (r *FeedRepository) GetByID(id int) (*model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at FROM feeds WHERE id = $1`
	return r.scanFeed(r.db.QueryRow(query, id))
}

func (r *FeedRepository) GetVisibleByUser(userID int) ([]model.Feed, error) {
	query := `
		SELECT f.id, f.url, f.title, f.last_fetched_at, f.fetch_interval_minutes, f.etag, f.last_modified, f.is_active, f.owner_id, f.feed_type, f.status, f.priority_weight, f.created_at,
		       COUNT(a.id) AS article_count,
		       COUNT(CASE WHEN COALESCE(rp.is_completed, false) = false THEN 1 END) AS unread_count
		FROM feeds f
		LEFT JOIN articles a ON a.feed_id = f.id
		LEFT JOIN reading_progress rp ON rp.article_id = a.id AND rp.user_id = $1
		WHERE f.owner_id IS NULL OR f.owner_id = $1
		GROUP BY f.id
		ORDER BY f.created_at DESC
	`
	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feeds []model.Feed
	for rows.Next() {
		var f model.Feed
		var title, etag, lastModified, feedType, status sql.NullString
		var ownerID sql.NullInt64
		err := rows.Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &ownerID, &feedType, &status, &f.PriorityWeight, &f.CreatedAt, &f.ArticleCount, &f.UnreadCount)
		if err != nil {
			return nil, err
		}
		f.Title = title.String
		f.ETag = etag.String
		f.LastModified = lastModified.String
		f.FeedType = feedType.String
		if f.FeedType == "" {
			f.FeedType = "rss"
		}
		f.Status = status.String
		if f.Status == "" {
			f.Status = "active"
		}
		if ownerID.Valid {
			oid := int(ownerID.Int64)
			f.OwnerID = &oid
		}
		feeds = append(feeds, f)
	}
	return feeds, nil
}

func (r *FeedRepository) Create(feed *model.Feed) error {
	feedType := feed.FeedType
	if feedType == "" {
		feedType = "rss"
	}
	query := `INSERT INTO feeds (url, title, fetch_interval_minutes, owner_id, feed_type) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`
	return r.db.QueryRow(query, feed.URL, feed.Title, feed.FetchIntervalMin, feed.OwnerID, feedType).Scan(&feed.ID, &feed.CreatedAt)
}

func (r *FeedRepository) Update(feed *model.Feed) error {
	query := `UPDATE feeds SET title = $1, is_active = $2 WHERE id = $3`
	_, err := r.db.Exec(query, feed.Title, feed.IsActive, feed.ID)
	return err
}

func (r *FeedRepository) Delete(id int) error {
	query := `DELETE FROM feeds WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}

func (r *FeedRepository) UpdateFetchInfo(id int, etag, lastModified string, fetchedAt time.Time) error {
	query := `UPDATE feeds SET etag = $1, last_modified = $2, last_fetched_at = $3 WHERE id = $4`
	_, err := r.db.Exec(query, etag, lastModified, fetchedAt, id)
	return err
}

func (r *FeedRepository) UpdateTitle(id int, title string) error {
	if title == "" {
		return nil
	}
	query := `UPDATE feeds SET title = $1 WHERE id = $2 AND (title IS NULL OR title = '')`
	_, err := r.db.Exec(query, title, id)
	return err
}

func (r *FeedRepository) GetAllActive() ([]model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at FROM feeds WHERE status = 'active' AND feed_type IN ('rss', 'html', 'youtube', 'podcast')`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanFeeds(rows)
}

// UpdateStatus changes a feed's lifecycle state. Mirrors to is_active for
// backward compat with existing queries: status='active' ↔ is_active=true,
// paused/archived ↔ is_active=false. The is_active column will be dropped
// after Phase 2 once all callers migrate.
func (r *FeedRepository) UpdateStatus(id int, status string) error {
	if status != "active" && status != "paused" && status != "archived" {
		return fmt.Errorf("invalid status: %s", status)
	}
	isActive := status == "active"
	_, err := r.db.Exec(
		`UPDATE feeds SET status = $1, is_active = $2 WHERE id = $3`,
		status, isActive, id,
	)
	return err
}

// UpdateWeight changes a feed's priority weight. Phase 1 stores only;
// Phase 2 verdict scoring multiplies by this value.
func (r *FeedRepository) UpdateWeight(id int, weight float64) error {
	if weight < 0 || weight > 2.0 {
		return fmt.Errorf("priority_weight must be in [0, 2.0]")
	}
	_, err := r.db.Exec(`UPDATE feeds SET priority_weight = $1 WHERE id = $2`, weight, id)
	return err
}

// GetOrCreateSavedFeed returns the user's "⭐ 网摘" feed, creating it if it
// doesn't exist. Saved feeds are the destination for articles captured via
// the browser bookmarklet when no existing article matches the captured URL.
// The url column has a global UNIQUE constraint, so we use a per-user
// sentinel of `bookmarklet://user/<id>`.
func (r *FeedRepository) GetOrCreateSavedFeed(ownerID int) (*model.Feed, error) {
	var f model.Feed
	var title, etag, lastModified, feedType, status sql.NullString
	var dbOwnerID sql.NullInt64
	err := r.db.QueryRow(
		`SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at
		 FROM feeds WHERE owner_id = $1 AND feed_type = 'saved'`,
		ownerID,
	).Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &dbOwnerID, &feedType, &status, &f.PriorityWeight, &f.CreatedAt)
	if err == nil {
		f.Title = title.String
		f.ETag = etag.String
		f.LastModified = lastModified.String
		f.FeedType = feedType.String
		f.Status = status.String
		if f.Status == "" {
			f.Status = "active"
		}
		if dbOwnerID.Valid {
			oid := int(dbOwnerID.Int64)
			f.OwnerID = &oid
		}
		return &f, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	owner := ownerID
	newFeed := &model.Feed{
		URL:              fmt.Sprintf("bookmarklet://user/%d", ownerID),
		Title:            "⭐ 网摘",
		FetchIntervalMin: 60,
		IsActive:         true,
		OwnerID:          &owner,
		FeedType:         "saved",
	}
	insertErr := r.db.QueryRow(
		`INSERT INTO feeds (url, title, fetch_interval_minutes, is_active, owner_id, feed_type)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		newFeed.URL, newFeed.Title, newFeed.FetchIntervalMin, newFeed.IsActive, newFeed.OwnerID, newFeed.FeedType,
	).Scan(&newFeed.ID, &newFeed.CreatedAt)
	if insertErr != nil {
		return nil, insertErr
	}
	return newFeed, nil
}
