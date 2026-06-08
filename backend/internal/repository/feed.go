package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
)

type FeedRepository struct {
	db Querier
}

func NewFeedRepository(db *sql.DB) *FeedRepository {
	return &FeedRepository{db: db}
}

// WithCtx returns a repository view bound to the per-request transaction
// stashed under ctxkey.Tx by RLSTxMiddleware. Falls back to the underlying
// handle if no tx is present.
func (r *FeedRepository) WithCtx(c ctxkey.CtxGetter) *FeedRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &FeedRepository{db: q}
		}
	}
	return r
}

func (r *FeedRepository) scanFeed(row *sql.Row) (*model.Feed, error) {
	var f model.Feed
	var title, etag, lastModified, feedType, status, providerSourceID sql.NullString
	var ownerID sql.NullInt64
	var expandLinks sql.NullBool
	err := row.Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &ownerID, &feedType, &status, &f.PriorityWeight, &f.CreatedAt, &expandLinks, &providerSourceID)
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
	f.ExpandLinks = expandLinks.Bool
	if providerSourceID.Valid {
		v := providerSourceID.String
		f.ProviderSourceID = &v
	}
	return &f, nil
}

func (r *FeedRepository) scanFeeds(rows *sql.Rows) ([]model.Feed, error) {
	var feeds []model.Feed
	for rows.Next() {
		var f model.Feed
		var title, etag, lastModified, feedType, status, providerSourceID sql.NullString
		var ownerID sql.NullInt64
		var expandLinks sql.NullBool
		err := rows.Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &ownerID, &feedType, &status, &f.PriorityWeight, &f.CreatedAt, &expandLinks, &providerSourceID)
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
		f.ExpandLinks = expandLinks.Bool
		if providerSourceID.Valid {
			v := providerSourceID.String
			f.ProviderSourceID = &v
		}
		feeds = append(feeds, f)
	}
	return feeds, nil
}

func (r *FeedRepository) GetAll() ([]model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at, expand_links, provider_source_id FROM feeds ORDER BY created_at DESC`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanFeeds(rows)
}

func (r *FeedRepository) GetByID(id int) (*model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at, expand_links, provider_source_id FROM feeds WHERE id = $1`
	return r.scanFeed(r.db.QueryRow(query, id))
}

func (r *FeedRepository) GetVisibleByUser(userID int) ([]model.Feed, error) {
	query := `
		SELECT f.id, f.url, f.title, f.last_fetched_at, f.fetch_interval_minutes, f.etag, f.last_modified, f.is_active, f.owner_id, f.feed_type, f.status, f.priority_weight, f.created_at, f.expand_links, f.provider_source_id,
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
		var title, etag, lastModified, feedType, status, providerSourceID sql.NullString
		var ownerID sql.NullInt64
		var expandLinks sql.NullBool
		err := rows.Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &ownerID, &feedType, &status, &f.PriorityWeight, &f.CreatedAt, &expandLinks, &providerSourceID, &f.ArticleCount, &f.UnreadCount)
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
		f.ExpandLinks = expandLinks.Bool
		if providerSourceID.Valid {
			v := providerSourceID.String
			f.ProviderSourceID = &v
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
	query := `INSERT INTO feeds (url, title, fetch_interval_minutes, owner_id, feed_type, expand_links) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`
	return r.db.QueryRow(query, feed.URL, feed.Title, feed.FetchIntervalMin, feed.OwnerID, feedType, feed.ExpandLinks).Scan(&feed.ID, &feed.CreatedAt)
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
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at, expand_links, provider_source_id FROM feeds WHERE status = 'active' AND feed_type IN ('rss', 'html', 'youtube', 'podcast')`
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

// GetOrCreateClipFeed returns the user's "⭐ 网摘" feed, creating it if it
// doesn't exist. Clip feeds are the destination for articles captured via
// the browser bookmarklet when no existing article matches the captured URL.
// The url column has a global UNIQUE constraint, so we use a per-user
// sentinel of `bookmarklet://user/<id>`.
func (r *FeedRepository) GetOrCreateClipFeed(ownerID int) (*model.Feed, error) {
	var f model.Feed
	var title, etag, lastModified, feedType, status, providerSourceID sql.NullString
	var dbOwnerID sql.NullInt64
	var expandLinks sql.NullBool
	err := r.db.QueryRow(
		`SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at, expand_links, provider_source_id
		 FROM feeds WHERE owner_id = $1 AND feed_type = 'clip'`,
		ownerID,
	).Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &dbOwnerID, &feedType, &status, &f.PriorityWeight, &f.CreatedAt, &expandLinks, &providerSourceID)
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
		f.ExpandLinks = expandLinks.Bool
		if providerSourceID.Valid {
			v := providerSourceID.String
			f.ProviderSourceID = &v
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
		FeedType:         "clip",
	}
	insertErr := r.db.QueryRow(
		`INSERT INTO feeds (url, title, fetch_interval_minutes, is_active, owner_id, feed_type, expand_links)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		newFeed.URL, newFeed.Title, newFeed.FetchIntervalMin, newFeed.IsActive, newFeed.OwnerID, newFeed.FeedType, false,
	).Scan(&newFeed.ID, &newFeed.CreatedAt)
	if insertErr != nil {
		return nil, insertErr
	}
	return newFeed, nil
}

// GetOrCreateByKindAndSource returns the feed identified by
// (owner, feed_type, provider_source_id), creating it if absent.
// Used by the extension ingest path for sources like twitter:list,
// twitter:user, twitter:bookmarks where provider_source_id is the
// list id, lowercased handle, or 'self' respectively.
//
// displayName is used only when creating the row.
func (r *FeedRepository) GetOrCreateByKindAndSource(
	ownerID int, feedType, sourceID, displayName string,
) (*model.Feed, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("GetOrCreateByKindAndSource: sourceID required")
	}

	var f model.Feed
	var title, etag, lastModified, dbFeedType, status sql.NullString
	var dbOwnerID sql.NullInt64
	var expandLinks sql.NullBool
	var providerSourceID sql.NullString

	err := r.db.QueryRow(
		`SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag,
		        last_modified, is_active, owner_id, feed_type, status,
		        priority_weight, created_at, expand_links, provider_source_id
		 FROM feeds
		 WHERE owner_id = $1 AND feed_type = $2 AND provider_source_id = $3`,
		ownerID, feedType, sourceID,
	).Scan(
		&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag,
		&lastModified, &f.IsActive, &dbOwnerID, &dbFeedType, &status,
		&f.PriorityWeight, &f.CreatedAt, &expandLinks, &providerSourceID,
	)
	if err == nil {
		f.Title = title.String
		f.ETag = etag.String
		f.LastModified = lastModified.String
		f.FeedType = dbFeedType.String
		f.Status = status.String
		if f.Status == "" {
			f.Status = "active"
		}
		if dbOwnerID.Valid {
			oid := int(dbOwnerID.Int64)
			f.OwnerID = &oid
		}
		f.ExpandLinks = expandLinks.Bool
		if providerSourceID.Valid {
			v := providerSourceID.String
			f.ProviderSourceID = &v
		}
		return &f, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	owner := ownerID
	name := displayName
	if name == "" {
		name = fmt.Sprintf("%s · %s", feedType, sourceID)
	}
	newFeed := &model.Feed{
		URL:              fmt.Sprintf("extension://%s/%d/%s", feedType, ownerID, sourceID),
		Title:            name,
		FetchIntervalMin: 60,
		IsActive:         true,
		OwnerID:          &owner,
		FeedType:         feedType,
		ProviderSourceID: &sourceID,
	}
	// Race-safe upsert. If a concurrent caller inserted the row between our
	// SELECT above and now, the INSERT will conflict on the unique partial
	// index (idx_feeds_owner_type_source, defined in migration 030 with
	// `WHERE provider_source_id IS NOT NULL`). DO NOTHING returns zero rows
	// in that case, and we re-read by recursing — the recursive SELECT path
	// will find the row immediately. The ON CONFLICT predicate must match
	// the partial index predicate exactly or PostgreSQL won't use the index.
	insertErr := r.db.QueryRow(
		`INSERT INTO feeds (url, title, fetch_interval_minutes, is_active, owner_id,
		                    feed_type, expand_links, provider_source_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (owner_id, feed_type, provider_source_id)
		   WHERE provider_source_id IS NOT NULL
		   DO NOTHING
		 RETURNING id, created_at`,
		newFeed.URL, newFeed.Title, newFeed.FetchIntervalMin, newFeed.IsActive,
		newFeed.OwnerID, newFeed.FeedType, false, *newFeed.ProviderSourceID,
	).Scan(&newFeed.ID, &newFeed.CreatedAt)
	if insertErr == sql.ErrNoRows {
		// Lost the race; another caller just inserted this row. Re-read it
		// via the same SELECT path at the top of the function.
		return r.GetOrCreateByKindAndSource(ownerID, feedType, sourceID, displayName)
	}
	if insertErr != nil {
		return nil, insertErr
	}
	return newFeed, nil
}
