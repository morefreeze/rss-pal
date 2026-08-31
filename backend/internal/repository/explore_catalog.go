package repository

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/util"
	"github.com/lib/pq"
)

var (
	ErrExploreArticleBatchTooLarge      = errors.New("explore article batch exceeds 50")
	ErrExploreCanonicalAdoptionConflict = errors.New("conflicting explore canonical adoption state")
)

const (
	maxExploreDueSources = 500

	exploreSourceColumns = `
		id, url, title, description, category, language, feed_type, is_broken,
		sort_order, site_url, normalized_url, validation_status, verified_at,
		last_checked_at, last_fetched_at, etag, last_modified, health_score,
		last_error, merged_into_source_id, first_discovered_at, last_observed_at, created_at`

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
		WHERE id=$1
		  AND (last_checked_at IS NULL OR last_checked_at <= $2)`

	exploreFetchSuccessSQL = `
		UPDATE recommended_feeds
		SET validation_status='valid', verified_at=$2, last_checked_at=$2,
		    last_fetched_at=$2, etag=NULLIF($3,''),
		    last_modified=NULLIF($4,''),
		    health_score=1, last_error=NULL, is_broken=false
		WHERE id=$1
		  AND (last_checked_at IS NULL OR last_checked_at <= $2)`

	exploreValidationValidSQL = `
		UPDATE recommended_feeds
		SET validation_status='valid', verified_at=$2, last_checked_at=$2,
		    etag=NULLIF($3,''), last_modified=NULLIF($4,''),
		    health_score=1, last_error=NULL, is_broken=false
		WHERE id=$1
		  AND (last_checked_at IS NULL OR last_checked_at <= $2)`

	exploreValidationInvalidSQL = `
		UPDATE recommended_feeds
		SET validation_status='invalid', last_checked_at=$2, health_score=0,
		    last_error=$3, is_broken=true
		WHERE id=$1
		  AND (last_checked_at IS NULL OR last_checked_at <= $2)`

	exploreFetchNotModifiedSQL = `
		UPDATE recommended_feeds
		SET validation_status='valid', verified_at=$2, last_checked_at=$2,
		    last_fetched_at=$2, health_score=1, last_error=NULL, is_broken=false
		WHERE id=$1
		  AND (last_checked_at IS NULL OR last_checked_at <= $2)`

	exploreCanonicalAdvisoryLockSQL = `SELECT pg_advisory_xact_lock($1)`

	exploreAdoptSourceLockSQL = `
		SELECT url, site_url, normalized_url
		FROM recommended_feeds WHERE id=$1 FOR UPDATE`

	exploreAdoptPairLockSQL = `
		SELECT id FROM recommended_feeds
		WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`

	exploreMergeObservationsSQL = `
		INSERT INTO explore_source_observations
		(provider_id, source_id, external_key, provider_tags, first_seen_at, last_seen_at, occurrence_count)
		SELECT provider_id, $2, external_key, provider_tags, first_seen_at, last_seen_at, occurrence_count
		FROM explore_source_observations WHERE source_id=$1
		ON CONFLICT (provider_id, external_key, source_id) DO UPDATE SET
			provider_tags = ARRAY(
				SELECT DISTINCT tag
				FROM unnest(explore_source_observations.provider_tags || EXCLUDED.provider_tags) AS tag
				ORDER BY tag
			),
			first_seen_at = LEAST(explore_source_observations.first_seen_at, EXCLUDED.first_seen_at),
			last_seen_at = GREATEST(explore_source_observations.last_seen_at, EXCLUDED.last_seen_at),
			occurrence_count = GREATEST(explore_source_observations.occurrence_count, EXCLUDED.occurrence_count)`

	exploreArticleUpsertSQL = `
		INSERT INTO explore_articles
		(source_id,url,normalized_url,title,content,excerpt,published_at,fetched_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (source_id,normalized_url) DO UPDATE SET
			url=CASE WHEN EXCLUDED.fetched_at >= explore_articles.fetched_at THEN EXCLUDED.url ELSE explore_articles.url END,
			title=CASE WHEN EXCLUDED.fetched_at >= explore_articles.fetched_at THEN EXCLUDED.title ELSE explore_articles.title END,
			content=CASE WHEN EXCLUDED.fetched_at >= explore_articles.fetched_at THEN COALESCE(EXCLUDED.content,explore_articles.content) ELSE explore_articles.content END,
			excerpt=CASE WHEN EXCLUDED.fetched_at >= explore_articles.fetched_at THEN COALESCE(EXCLUDED.excerpt,explore_articles.excerpt) ELSE explore_articles.excerpt END,
			published_at=CASE WHEN EXCLUDED.fetched_at >= explore_articles.fetched_at THEN COALESCE(EXCLUDED.published_at,explore_articles.published_at) ELSE explore_articles.published_at END,
			fetched_at=CASE WHEN EXCLUDED.fetched_at >= explore_articles.fetched_at THEN EXCLUDED.fetched_at ELSE explore_articles.fetched_at END,
			updated_at=CASE WHEN EXCLUDED.fetched_at >= explore_articles.fetched_at THEN CURRENT_TIMESTAMP ELSE explore_articles.updated_at END
		RETURNING id`

	exploreSourceWriteLockSQL = `
		SELECT id FROM recommended_feeds WHERE id=$1 FOR UPDATE`

	exploreDueSourcesSQL = `
		SELECT ` + exploreSourceColumns + `
		FROM recommended_feeds source
		WHERE EXISTS (
			SELECT 1
			FROM explore_source_observations observation
			JOIN explore_registry_providers provider ON provider.id=observation.provider_id
			WHERE observation.source_id = source.id AND provider.enabled
		) AND ((
			source.validation_status = 'pending'
			AND (source.last_checked_at IS NULL OR source.last_checked_at <= $1)
		) OR (
			source.validation_status = 'valid'
			AND (COALESCE(source.last_checked_at,source.last_fetched_at) IS NULL
			     OR COALESCE(source.last_checked_at,source.last_fetched_at) <= $2)
		))
		ORDER BY CASE WHEN source.validation_status = 'pending' THEN 0 ELSE 1 END,
		         CASE WHEN source.validation_status = 'pending' THEN source.last_checked_at ELSE COALESCE(source.last_checked_at,source.last_fetched_at) END ASC NULLS FIRST,
		         source.id ASC
		LIMIT $3`
)

// ExploreCatalogObservation joins public evidence to its provider metadata.
// SourceID always points at the canonical recommended_feeds row.
type ExploreCatalogObservation struct {
	model.ExploreSourceObservation
	ProviderKey           string
	ProviderKind          string
	ProviderEnabled       bool
	ProviderTopic         *string
	ProviderLastSuccessAt *time.Time
}

type ExploreCatalogSource struct {
	Source       model.ExploreSource
	Observations []ExploreCatalogObservation
}

type exploreAdoptionDecision string

const (
	exploreAdoptMergeIntoTarget exploreAdoptionDecision = "merge_into_target"
	exploreAdoptPreserveSource  exploreAdoptionDecision = "preserve_source"
	exploreAdoptReturnTarget    exploreAdoptionDecision = "return_target"
	exploreAdoptFailClosed      exploreAdoptionDecision = "fail_closed"
)

type exploreAdoptionSourceState struct {
	NormalizedURL      string
	ValidationStatus   string
	MergedIntoSourceID *int
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
		       provider.provider_kind, provider.enabled, provider.topic, provider.last_success_at
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
			&observation.ProviderKey, &observation.ProviderKind, &observation.ProviderEnabled,
			&observation.ProviderTopic, &observation.ProviderLastSuccessAt,
		); err != nil {
			return nil, err
		}
		observation.ProviderTags = []string(tags)
		result.Observations = append(result.Observations, observation)
	}
	return result, rows.Err()
}

// ListDueSources returns observed canonical rows only. Pending sources use
// their validation clock; valid sources use the latest check/fetch clock.
func (r *ExploreCatalogRepository) ListDueSources(validationDueBefore, refreshDueBefore time.Time, limit int) ([]model.ExploreSource, error) {
	if limit <= 0 {
		return []model.ExploreSource{}, nil
	}
	if limit > maxExploreDueSources {
		limit = maxExploreDueSources
	}
	rows, err := r.db.Query(exploreDueSourcesSQL, validationDueBefore, refreshDueBefore, limit)
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
	result, err := r.db.Exec(exploreValidationValidSQL, sourceID, checkedAt, etag, lastModified)
	return expectExploreSourceMonotonicUpdate(r.db, result, err, sourceID)
}

// MarkValidationInvalid records a terminal validation outcome without
// removing conditional request state or the last successfully cached rows.
func (r *ExploreCatalogRepository) MarkValidationInvalid(sourceID int, checkedAt time.Time, cause error) error {
	result, err := r.db.Exec(exploreValidationInvalidSQL, sourceID, checkedAt, ClipExploreError(cause))
	return expectExploreSourceMonotonicUpdate(r.db, result, err, sourceID)
}

// RecordFetchFailure atomically degrades health. Four consecutive failures
// from a healthy source make it ineligible, while its last-good cache remains.
func (r *ExploreCatalogRepository) RecordFetchFailure(sourceID int, checkedAt time.Time, cause error) error {
	result, err := r.db.Exec(exploreFetchFailureSQL, sourceID, checkedAt, ClipExploreError(cause))
	return expectExploreSourceMonotonicUpdate(r.db, result, err, sourceID)
}

func (r *ExploreCatalogRepository) RecordFetchSuccess(sourceID int, fetchedAt time.Time, etag, lastModified string) error {
	result, err := r.db.Exec(exploreFetchSuccessSQL, sourceID, fetchedAt, etag, lastModified)
	return expectExploreSourceMonotonicUpdate(r.db, result, err, sourceID)
}

// RecordFetchNotModified records a successful 304 without changing the
// validators saved from the last 200 response.
func (r *ExploreCatalogRepository) RecordFetchNotModified(sourceID int, fetchedAt time.Time) error {
	result, err := r.db.Exec(exploreFetchNotModifiedSQL, sourceID, fetchedAt)
	return expectExploreSourceMonotonicUpdate(r.db, result, err, sourceID)
}

// AdoptDiscoveredFeed atomically replaces a discovery URL with its canonical
// feed URL or merges its public evidence into an already-known canonical row.
// It joins an outer transaction when the repository is transaction-bound.
func (r *ExploreCatalogRepository) AdoptDiscoveredFeed(sourceID int, canonicalFeedURL string) (canonicalID int, merged bool, err error) {
	if sourceID <= 0 {
		return 0, false, errors.New("explore source id must be positive")
	}
	if err := validateExploreCanonicalFeedURL(canonicalFeedURL); err != nil {
		return 0, false, err
	}
	q, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return 0, false, err
	}
	defer rollback()
	if err := lockExploreCanonicalURL(q, canonicalFeedURL); err != nil {
		return 0, false, err
	}

	var currentNormalized string
	if err := q.QueryRow(`SELECT normalized_url FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&currentNormalized); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, exploreSourceNotFoundError(sourceID)
		}
		return 0, false, err
	}
	if currentNormalized == canonicalFeedURL {
		if err := lockExploreSourceForWrite(q, sourceID); err != nil {
			return 0, false, err
		}
		if err := commit(); err != nil {
			return 0, false, err
		}
		return sourceID, false, nil
	}

	var targetID int
	targetErr := q.QueryRow(`SELECT id FROM recommended_feeds WHERE normalized_url=$1`, canonicalFeedURL).Scan(&targetID)
	if targetErr != nil && !errors.Is(targetErr, sql.ErrNoRows) {
		return 0, false, targetErr
	}
	if errors.Is(targetErr, sql.ErrNoRows) {
		var oldURL string
		var oldSiteURL sql.NullString
		if err := q.QueryRow(exploreAdoptSourceLockSQL, sourceID).Scan(&oldURL, &oldSiteURL, &currentNormalized); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, false, exploreSourceNotFoundError(sourceID)
			}
			return 0, false, err
		}
		if currentNormalized != canonicalFeedURL {
			result, err := q.Exec(`
				UPDATE recommended_feeds
				SET site_url=CASE WHEN url <> $2 AND site_url IS NULL THEN url ELSE site_url END,
				    url=$2, normalized_url=$2
				WHERE id=$1`, sourceID, canonicalFeedURL)
			if err := expectExploreSourceUpdate(result, err, sourceID); err != nil {
				return 0, false, err
			}
		}
		if err := commit(); err != nil {
			return 0, false, err
		}
		return sourceID, false, nil
	}
	if targetID == sourceID {
		if err := lockExploreSourceForWrite(q, sourceID); err != nil {
			return 0, false, err
		}
		if err := commit(); err != nil {
			return 0, false, err
		}
		return sourceID, false, nil
	}
	if err := lockExploreSourcePair(q, sourceID, targetID); err != nil {
		return 0, false, err
	}
	// Reconfirm both rows after acquiring the deterministic pair lock. Reverse
	// A->B and B->A adoptions may have invalidated one side while this
	// transaction waited; never merge the surviving row back into that loser.
	sourceState, err := loadExploreAdoptionSourceState(q, sourceID)
	if err != nil {
		return 0, false, err
	}
	targetState, err := loadExploreAdoptionSourceState(q, targetID)
	if err != nil {
		return 0, false, err
	}
	if targetState.NormalizedURL != canonicalFeedURL {
		result, err := q.Exec(`
			UPDATE recommended_feeds
			SET site_url=CASE WHEN url <> $2 AND site_url IS NULL THEN url ELSE site_url END,
			    url=$2, normalized_url=$2
			WHERE id=$1`, sourceID, canonicalFeedURL)
		if err := expectExploreSourceUpdate(result, err, sourceID); err != nil {
			return 0, false, err
		}
		if err := commit(); err != nil {
			return 0, false, err
		}
		return sourceID, false, nil
	}
	switch decideExploreAdoption(sourceID, targetID, sourceState, targetState) {
	case exploreAdoptPreserveSource:
		if err := commit(); err != nil {
			return 0, false, err
		}
		return sourceID, false, nil
	case exploreAdoptReturnTarget:
		if err := commit(); err != nil {
			return 0, false, err
		}
		return targetID, true, nil
	case exploreAdoptFailClosed:
		return 0, false, fmt.Errorf("%w: source %d target %d", ErrExploreCanonicalAdoptionConflict, sourceID, targetID)
	}
	if _, err := q.Exec(exploreMergeObservationsSQL, sourceID, targetID); err != nil {
		return 0, false, err
	}
	if _, err := q.Exec(`DELETE FROM explore_source_observations WHERE source_id=$1`, sourceID); err != nil {
		return 0, false, err
	}
	mergeMessage := fmt.Sprintf("merged into explore source %d", targetID)
	result, err := q.Exec(`
		UPDATE recommended_feeds
		SET validation_status='invalid', health_score=0, is_broken=true,
		    last_error=$2, merged_into_source_id=$3
		WHERE id=$1`, sourceID, mergeMessage, targetID)
	if err := expectExploreSourceUpdate(result, err, sourceID); err != nil {
		return 0, false, err
	}
	if err := commit(); err != nil {
		return 0, false, err
	}
	return targetID, true, nil
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
	if len(articles) > 50 {
		return ErrExploreArticleBatchTooLarge
	}
	if sourceID <= 0 {
		return errors.New("explore source id must be positive")
	}
	q, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return err
	}
	defer rollback()
	txRepo := r.WithQuerier(q)
	if err := lockExploreSourceForWrite(q, sourceID); err != nil {
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
	if err := retainExploreArticles(q, sourceID, retainedAt); err != nil {
		return err
	}
	return commit()
}

func (r *ExploreCatalogRepository) RetainArticles(sourceID int, retainedAt time.Time) error {
	if sourceID <= 0 {
		return errors.New("explore source id must be positive")
	}
	q, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return err
	}
	defer rollback()
	if err := lockExploreSourceForWrite(q, sourceID); err != nil {
		return err
	}
	if err := retainExploreArticles(q, sourceID, retainedAt); err != nil {
		return err
	}
	return commit()
}

func retainExploreArticles(q Querier, sourceID int, retainedAt time.Time) error {
	_, err := q.Exec(exploreArticleRetentionSQL, sourceID, retainedAt.Add(-30*24*time.Hour))
	return err
}

func lockExploreSourceForWrite(q Querier, sourceID int) error {
	var lockedSourceID int
	err := q.QueryRow(exploreSourceWriteLockSQL, sourceID).Scan(&lockedSourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return exploreSourceNotFoundError(sourceID)
	}
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
		&source.HealthScore, &lastError, &source.MergedIntoSourceID, &source.FirstDiscoveredAt,
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
		return exploreSourceNotFoundError(sourceID)
	}
	return nil
}

func expectExploreSourceMonotonicUpdate(q Querier, result sql.Result, err error, sourceID int) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	if count != 0 {
		return fmt.Errorf("explore source %d mutation affected %d rows", sourceID, count)
	}
	var exists bool
	if err := q.QueryRow(`SELECT EXISTS (SELECT 1 FROM recommended_feeds WHERE id=$1)`, sourceID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	return exploreSourceNotFoundError(sourceID)
}

func lockExploreCanonicalURL(q Querier, canonicalURL string) error {
	digest := sha256.Sum256([]byte(canonicalURL))
	lockKey := int64(binary.BigEndian.Uint64(digest[:8]))
	var ignored interface{}
	return q.QueryRow(exploreCanonicalAdvisoryLockSQL, lockKey).Scan(&ignored)
}

func lockExploreSourcePair(q Querier, firstID, secondID int) error {
	rows, err := q.Query(exploreAdoptPairLockSQL, firstID, secondID)
	if err != nil {
		return err
	}
	defer rows.Close()
	locked := 0
	for rows.Next() {
		var sourceID int
		if err := rows.Scan(&sourceID); err != nil {
			return err
		}
		locked++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if locked != 2 {
		return fmt.Errorf("explore source pair %d,%d not found", firstID, secondID)
	}
	return nil
}

func loadExploreAdoptionSourceState(q Querier, sourceID int) (exploreAdoptionSourceState, error) {
	var state exploreAdoptionSourceState
	err := q.QueryRow(`SELECT normalized_url, validation_status, merged_into_source_id FROM recommended_feeds WHERE id=$1`, sourceID).Scan(&state.NormalizedURL, &state.ValidationStatus, &state.MergedIntoSourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return exploreAdoptionSourceState{}, exploreSourceNotFoundError(sourceID)
	}
	return state, err
}

func decideExploreAdoption(sourceID, targetID int, source, target exploreAdoptionSourceState) exploreAdoptionDecision {
	if source.MergedIntoSourceID != nil || target.MergedIntoSourceID != nil {
		if source.MergedIntoSourceID != nil && target.MergedIntoSourceID == nil && *source.MergedIntoSourceID == targetID {
			return exploreAdoptReturnTarget
		}
		if source.MergedIntoSourceID == nil && target.MergedIntoSourceID != nil && *target.MergedIntoSourceID == sourceID {
			return exploreAdoptPreserveSource
		}
		return exploreAdoptFailClosed
	}
	if source.ValidationStatus == model.ExploreValidationInvalid && target.ValidationStatus == model.ExploreValidationInvalid {
		return exploreAdoptFailClosed
	}
	return exploreAdoptMergeIntoTarget
}

func validateExploreCanonicalFeedURL(raw string) error {
	if raw == "" || len(raw) > 2048 || strings.ContainsRune(raw, '\x00') {
		return errors.New("canonical feed URL is empty, too long, or contains NUL")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid canonical feed URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("canonical feed URL must use HTTP or HTTPS")
	}
	if parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil {
		return errors.New("canonical feed URL must have a host and no credentials")
	}
	if util.NormalizeURL(raw) != raw {
		return errors.New("canonical feed URL must already be normalized")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("canonical feed URL cannot use localhost")
	}
	if address := net.ParseIP(host); address != nil {
		if address.IsPrivate() || address.IsLoopback() || address.IsUnspecified() ||
			address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
			return errors.New("canonical feed URL cannot use a private or unsafe IP literal")
		}
	}
	return nil
}

func exploreSourceNotFoundError(sourceID int) error {
	return fmt.Errorf("explore source %d not found", sourceID)
}
