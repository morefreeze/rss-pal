package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/lib/pq"
)

const (
	maxExploreDueSources = 500

	exploreSourceColumns = `
		id, url, title, description, category, language, feed_type, is_broken,
		sort_order, site_url, normalized_url, validation_status, verified_at,
		last_checked_at, last_fetched_at, etag, last_modified, health_score,
		last_error, first_discovered_at, last_observed_at, created_at`

	exploreArticleRetentionSQL = `
		WITH ranked AS (
			SELECT article.id,
			       ROW_NUMBER() OVER (
				   ORDER BY COALESCE(article.published_at, article.fetched_at) DESC,
				            article.fetched_at DESC, article.id DESC
			   ) AS position,
			       COALESCE(article.published_at, article.fetched_at) AS effective_at
			FROM explore_articles article
			WHERE article.source_id = $1
		)
		DELETE FROM explore_articles article
		USING ranked, recommended_feeds source
		WHERE article.id = ranked.id
		  AND source.id = $1
		  AND (
			ranked.position > 50
			OR (
				ranked.effective_at < $2
				AND (source.validation_status <> 'valid' OR ranked.position > 5)
			)
		  )`

	exploreFetchFailureSQL = `
		UPDATE recommended_feeds
		SET last_checked_at=$2,
		    health_score=GREATEST(0,COALESCE(health_score,1)-0.25),
		    is_broken=(GREATEST(0,COALESCE(health_score,1)-0.25)=0),
		    last_error=$3
		WHERE id=$1`

	exploreFetchSuccessSQL = `
		UPDATE recommended_feeds
		SET validation_status='valid', verified_at=$2, last_checked_at=$2,
		    last_fetched_at=$2, etag=COALESCE(NULLIF($3,''),etag),
		    last_modified=COALESCE(NULLIF($4,''),last_modified),
		    health_score=1, last_error=NULL, is_broken=false
		WHERE id=$1`

	exploreArticleUpsertSQL = `
		INSERT INTO explore_articles
		(source_id,url,normalized_url,title,content,excerpt,published_at,fetched_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (source_id,normalized_url) DO UPDATE SET
			url=EXCLUDED.url, title=EXCLUDED.title,
			content=COALESCE(EXCLUDED.content,explore_articles.content),
			excerpt=COALESCE(EXCLUDED.excerpt,explore_articles.excerpt),
			published_at=COALESCE(EXCLUDED.published_at,explore_articles.published_at),
			fetched_at=EXCLUDED.fetched_at, updated_at=CURRENT_TIMESTAMP
		RETURNING id`

	exploreSourceWriteLockSQL = `
		SELECT id FROM recommended_feeds WHERE id=$1 FOR UPDATE`
)

// ExploreCatalogObservation joins public evidence to its provider metadata.
// SourceID always points at the canonical recommended_feeds row.
type ExploreCatalogObservation struct {
	model.ExploreSourceObservation
	ProviderKey           string
	ProviderKind          string
	ProviderTopic         *string
	ProviderLastSuccessAt *time.Time
}

type ExploreCatalogSource struct {
	Source       model.ExploreSource
	Observations []ExploreCatalogObservation
}

// ExploreCatalogRepository persists the shared Explore catalog. Querier keeps
// every operation usable with either a pool or an existing worker transaction.
type ExploreCatalogRepository struct{ db Querier }

func NewExploreCatalogRepository(db Querier) *ExploreCatalogRepository {
	return &ExploreCatalogRepository{db: db}
}

func (r *ExploreCatalogRepository) WithQuerier(db Querier) *ExploreCatalogRepository {
	return &ExploreCatalogRepository{db: db}
}

func (r *ExploreCatalogRepository) GetSource(sourceID int) (*model.ExploreSource, error) {
	if sourceID <= 0 {
		return nil, errors.New("explore source id must be positive")
	}
	return scanExploreSource(r.db.QueryRow(`SELECT `+exploreSourceColumns+` FROM recommended_feeds WHERE id=$1`, sourceID))
}

func (r *ExploreCatalogRepository) GetSourceWithObservations(sourceID int) (*ExploreCatalogSource, error) {
	source, err := r.GetSource(sourceID)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(`
		SELECT observation.id, observation.provider_id, observation.source_id,
		       observation.external_key, observation.provider_tags,
		       observation.first_seen_at, observation.last_seen_at,
		       observation.occurrence_count, provider.provider_key,
		       provider.provider_kind, provider.topic, provider.last_success_at
		FROM explore_source_observations observation
		JOIN explore_registry_providers provider ON provider.id = observation.provider_id
		WHERE observation.source_id = $1
		ORDER BY observation.last_seen_at DESC, observation.id DESC`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &ExploreCatalogSource{Source: *source, Observations: []ExploreCatalogObservation{}}
	for rows.Next() {
		var observation ExploreCatalogObservation
		var tags pq.StringArray
		if err := rows.Scan(
			&observation.ID, &observation.ProviderID, &observation.SourceID,
			&observation.ExternalKey, &tags, &observation.FirstSeenAt,
			&observation.LastSeenAt, &observation.OccurrenceCount,
			&observation.ProviderKey, &observation.ProviderKind,
			&observation.ProviderTopic, &observation.ProviderLastSuccessAt,
		); err != nil {
			return nil, err
		}
		observation.ProviderTags = []string(tags)
		result.Observations = append(result.Observations, observation)
	}
	return result, rows.Err()
}

// ListDueSources returns canonical source rows only. Non-valid sources use
// their health-check clock; valid sources use their last successful fetch.
func (r *ExploreCatalogRepository) ListDueSources(validationDueBefore, refreshDueBefore time.Time, limit int) ([]model.ExploreSource, error) {
	if limit <= 0 {
		return []model.ExploreSource{}, nil
	}
	if limit > maxExploreDueSources {
		limit = maxExploreDueSources
	}
	rows, err := r.db.Query(`
		SELECT `+exploreSourceColumns+`
		FROM recommended_feeds
		WHERE (
			validation_status = 'pending'
			AND (last_checked_at IS NULL OR last_checked_at <= $1)
		) OR (
			validation_status = 'valid'
			AND (COALESCE(last_checked_at,last_fetched_at) IS NULL
			     OR COALESCE(last_checked_at,last_fetched_at) <= $2)
		)
		ORDER BY CASE WHEN validation_status = 'pending' THEN 0 ELSE 1 END,
		         CASE WHEN validation_status = 'pending' THEN last_checked_at ELSE COALESCE(last_checked_at,last_fetched_at) END ASC NULLS FIRST,
		         id ASC
		LIMIT $3`, validationDueBefore, refreshDueBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.ExploreSource{}
	for rows.Next() {
		source, err := scanExploreSource(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *source)
	}
	return result, rows.Err()
}

func (r *ExploreCatalogRepository) MarkValidationPending(sourceID int) error {
	result, err := r.db.Exec(`
		UPDATE recommended_feeds
		SET validation_status='pending', is_broken=false, last_error=NULL
		WHERE id=$1`, sourceID)
	return expectExploreSourceUpdate(result, err, sourceID)
}

func (r *ExploreCatalogRepository) MarkValidationValid(sourceID int, checkedAt time.Time, etag, lastModified string) error {
	result, err := r.db.Exec(`
		UPDATE recommended_feeds
		SET validation_status='valid', verified_at=$2, last_checked_at=$2,
		    etag=COALESCE(NULLIF($3,''),etag),
		    last_modified=COALESCE(NULLIF($4,''),last_modified),
		    health_score=1, last_error=NULL, is_broken=false
		WHERE id=$1`, sourceID, checkedAt, etag, lastModified)
	return expectExploreSourceUpdate(result, err, sourceID)
}

// MarkValidationInvalid records a terminal validation outcome without
// removing conditional request state or the last successfully cached rows.
func (r *ExploreCatalogRepository) MarkValidationInvalid(sourceID int, checkedAt time.Time, cause error) error {
	result, err := r.db.Exec(`
		UPDATE recommended_feeds
		SET validation_status='invalid', last_checked_at=$2, health_score=0,
		    last_error=$3, is_broken=true
		WHERE id=$1`, sourceID, checkedAt, ClipExploreError(cause))
	return expectExploreSourceUpdate(result, err, sourceID)
}

// RecordFetchFailure atomically degrades health. Four consecutive failures
// from a healthy source make it ineligible, while its last-good cache remains.
func (r *ExploreCatalogRepository) RecordFetchFailure(sourceID int, checkedAt time.Time, cause error) error {
	result, err := r.db.Exec(exploreFetchFailureSQL, sourceID, checkedAt, ClipExploreError(cause))
	return expectExploreSourceUpdate(result, err, sourceID)
}

func (r *ExploreCatalogRepository) RecordFetchSuccess(sourceID int, fetchedAt time.Time, etag, lastModified string) error {
	result, err := r.db.Exec(exploreFetchSuccessSQL, sourceID, fetchedAt, etag, lastModified)
	return expectExploreSourceUpdate(result, err, sourceID)
}

func (r *ExploreCatalogRepository) UpsertArticle(article model.ExploreArticle) (int, error) {
	if article.SourceID <= 0 || article.URL == "" || article.NormalizedURL == "" || article.Title == "" {
		return 0, errors.New("source, URL, normalized URL, and title are required")
	}
	if article.FetchedAt.IsZero() {
		return 0, errors.New("fetched time is required")
	}
	var articleID int
	err := r.db.QueryRow(exploreArticleUpsertSQL, article.SourceID, article.URL, article.NormalizedURL,
		article.Title, article.Content, article.Excerpt, article.PublishedAt,
		article.FetchedAt).Scan(&articleID)
	return articleID, err
}

// UpsertArticles atomically updates one source and applies cache retention when
// called with a pool. When already bound to a transaction it joins that outer
// transaction without taking ownership of commit or rollback.
func (r *ExploreCatalogRepository) UpsertArticles(sourceID int, articles []model.ExploreArticle, retainedAt time.Time) error {
	if sourceID <= 0 {
		return errors.New("explore source id must be positive")
	}
	q, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return err
	}
	defer rollback()
	txRepo := r.WithQuerier(q)
	var lockedSourceID int
	if err := q.QueryRow(exploreSourceWriteLockSQL, sourceID).Scan(&lockedSourceID); err != nil {
		return err
	}
	for index := range articles {
		if articles[index].SourceID == 0 {
			articles[index].SourceID = sourceID
		}
		if articles[index].SourceID != sourceID {
			return fmt.Errorf("explore article source %d does not match catalog source %d", articles[index].SourceID, sourceID)
		}
		if _, err := txRepo.UpsertArticle(articles[index]); err != nil {
			return err
		}
	}
	if err := txRepo.RetainArticles(sourceID, retainedAt); err != nil {
		return err
	}
	return commit()
}

func (r *ExploreCatalogRepository) RetainArticles(sourceID int, retainedAt time.Time) error {
	if sourceID <= 0 {
		return errors.New("explore source id must be positive")
	}
	_, err := r.db.Exec(exploreArticleRetentionSQL, sourceID, retainedAt.Add(-30*24*time.Hour))
	return err
}

func (r *ExploreCatalogRepository) ListArticles(sourceID, limit int) ([]model.ExploreArticle, error) {
	if sourceID <= 0 {
		return nil, errors.New("explore source id must be positive")
	}
	if limit <= 0 {
		return []model.ExploreArticle{}, nil
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := r.db.Query(`
		SELECT id,source_id,url,normalized_url,title,content,excerpt,published_at,
		       fetched_at,created_at,updated_at
		FROM explore_articles WHERE source_id=$1
		ORDER BY COALESCE(published_at,fetched_at) DESC,fetched_at DESC,id DESC
		LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	articles := []model.ExploreArticle{}
	for rows.Next() {
		var article model.ExploreArticle
		if err := rows.Scan(&article.ID, &article.SourceID, &article.URL,
			&article.NormalizedURL, &article.Title, &article.Content,
			&article.Excerpt, &article.PublishedAt, &article.FetchedAt,
			&article.CreatedAt, &article.UpdatedAt); err != nil {
			return nil, err
		}
		articles = append(articles, article)
	}
	return articles, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanExploreSource(row rowScanner) (*model.ExploreSource, error) {
	var source model.ExploreSource
	var description, siteURL, etag, lastModified, lastError sql.NullString
	var feedType sql.NullString
	var isBroken sql.NullBool
	var sortOrder sql.NullInt64
	err := row.Scan(
		&source.ID, &source.URL, &source.Title, &description, &source.Category,
		&source.Language, &feedType, &isBroken, &sortOrder, &siteURL,
		&source.NormalizedURL, &source.ValidationStatus, &source.VerifiedAt,
		&source.LastCheckedAt, &source.LastFetchedAt, &etag, &lastModified,
		&source.HealthScore, &lastError, &source.FirstDiscoveredAt,
		&source.LastObservedAt, &source.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	source.FeedType = feedType.String
	if source.FeedType == "" {
		source.FeedType = "rss"
	}
	source.IsBroken = isBroken.Bool
	source.SortOrder = int(sortOrder.Int64)
	source.Description = exploreNullableStringPointer(description)
	source.SiteURL = exploreNullableStringPointer(siteURL)
	source.ETag = exploreNullableStringPointer(etag)
	source.LastModified = exploreNullableStringPointer(lastModified)
	source.LastError = exploreNullableStringPointer(lastError)
	return &source, nil
}

func exploreNullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func expectExploreSourceUpdate(result sql.Result, err error, sourceID int) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("explore source %d not found", sourceID)
	}
	return nil
}
