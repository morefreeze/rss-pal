package repository

import (
	"database/sql"
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
	var title, etag, lastModified sql.NullString
	var ownerID sql.NullInt64
	err := row.Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &ownerID, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	f.Title = title.String
	f.ETag = etag.String
	f.LastModified = lastModified.String
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
		var title, etag, lastModified sql.NullString
		var ownerID sql.NullInt64
		err := rows.Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &ownerID, &f.CreatedAt)
		if err != nil {
			return nil, err
		}
		f.Title = title.String
		f.ETag = etag.String
		f.LastModified = lastModified.String
		if ownerID.Valid {
			oid := int(ownerID.Int64)
			f.OwnerID = &oid
		}
		feeds = append(feeds, f)
	}
	return feeds, nil
}

func (r *FeedRepository) GetAll() ([]model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, created_at FROM feeds ORDER BY created_at DESC`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanFeeds(rows)
}

func (r *FeedRepository) GetByID(id int) (*model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, created_at FROM feeds WHERE id = $1`
	return r.scanFeed(r.db.QueryRow(query, id))
}

func (r *FeedRepository) GetVisibleByUser(userID int) ([]model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, created_at FROM feeds WHERE owner_id IS NULL OR owner_id = $1 ORDER BY created_at DESC`
	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanFeeds(rows)
}

func (r *FeedRepository) Create(feed *model.Feed) error {
	query := `INSERT INTO feeds (url, title, fetch_interval_minutes, owner_id) VALUES ($1, $2, $3, $4) RETURNING id, created_at`
	return r.db.QueryRow(query, feed.URL, feed.Title, feed.FetchIntervalMin, feed.OwnerID).Scan(&feed.ID, &feed.CreatedAt)
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
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, created_at FROM feeds WHERE is_active = true`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanFeeds(rows)
}
