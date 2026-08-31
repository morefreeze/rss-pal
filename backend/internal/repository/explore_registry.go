package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/lib/pq"
)

// ExploreRegistryRepository persists public provider state and observations.
type ExploreRegistryRepository struct{ db Querier }

func NewExploreRegistryRepository(db *sql.DB) *ExploreRegistryRepository {
	return &ExploreRegistryRepository{db: db}
}

func (r *ExploreRegistryRepository) WithCtx(c ctxkey.CtxGetter) *ExploreRegistryRepository {
	if value, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := value.(Querier); ok {
			return &ExploreRegistryRepository{db: q}
		}
	}
	return r
}

func (r *ExploreRegistryRepository) LoadDueProviders(now time.Time) ([]explore.RegistryProvider, error) {
	rows, err := r.db.Query(`
		SELECT id, provider_key, provider_kind, endpoint, topic, sync_interval_minutes,
		       enabled, etag, last_modified, last_sync_at, last_success_at, consecutive_failures
		FROM explore_registry_providers
		WHERE enabled
		  AND (last_sync_at IS NULL OR last_sync_at + sync_interval_minutes * POWER(2, LEAST(consecutive_failures, 6)) * INTERVAL '1 minute' <= $1)
		ORDER BY id ASC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	providers := []explore.RegistryProvider{}
	for rows.Next() {
		var provider explore.RegistryProvider
		var interval int
		var topic, etag, modified sql.NullString
		if err := rows.Scan(&provider.ID, &provider.Key, &provider.Kind, &provider.Endpoint, &topic, &interval, &provider.Enabled, &etag, &modified, &provider.LastSyncAt, &provider.LastSuccessAt, &provider.ConsecutiveFailures); err != nil {
			return nil, err
		}
		provider.Topic, provider.ETag, provider.LastModified = topic.String, etag.String, modified.String
		provider.SyncInterval = time.Duration(interval) * time.Minute
		providers = append(providers, provider)
	}
	return providers, rows.Err()
}

func (r *ExploreRegistryRepository) UpsertCandidate(providerID int, candidate explore.Candidate, observedAt time.Time) (int, error) {
	if err := explore.ValidateCandidate(candidate); err != nil {
		return 0, fmt.Errorf("invalid explore candidate: %w", err)
	}
	if providerID <= 0 || candidate.ExternalKey == "" || candidate.FeedURL == "" {
		return 0, errors.New("provider, external key, and feed URL are required")
	}
	if candidate.OccurrenceCount < 1 {
		candidate.OccurrenceCount = 1
	}
	title := candidate.Title
	if title == "" {
		title = candidate.FeedURL
	}
	category := candidate.Topic
	if category == "" {
		category = "general"
	}
	q, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return 0, err
	}
	defer rollback()
	var sourceID int
	err = q.QueryRow(`
		INSERT INTO recommended_feeds (url, title, category, language, feed_type, site_url, normalized_url, validation_status, first_discovered_at, last_observed_at)
		VALUES ($1, $2, $3, 'en', 'rss', NULLIF($4, ''), $1, 'pending', $5, $5)
		ON CONFLICT (normalized_url) DO UPDATE SET
			title = CASE WHEN NULLIF(EXCLUDED.title, '') IS NULL THEN recommended_feeds.title ELSE EXCLUDED.title END,
			site_url = COALESCE(NULLIF(EXCLUDED.site_url, ''), recommended_feeds.site_url),
			category = COALESCE(NULLIF(EXCLUDED.category, ''), recommended_feeds.category),
			last_observed_at = EXCLUDED.last_observed_at
		RETURNING id`, candidate.FeedURL, title, category, candidate.SiteURL, observedAt).Scan(&sourceID)
	if err != nil {
		return 0, err
	}
	_, err = q.Exec(`
		INSERT INTO explore_source_observations (provider_id, source_id, external_key, provider_tags, first_seen_at, last_seen_at, occurrence_count)
		VALUES ($1, $2, $3, $4, $5, $5, $6)
		ON CONFLICT (provider_id, external_key, source_id) DO UPDATE SET
			provider_tags=EXCLUDED.provider_tags, last_seen_at=EXCLUDED.last_seen_at, occurrence_count=EXCLUDED.occurrence_count`,
		providerID, sourceID, candidate.ExternalKey, pq.Array(candidate.Tags), observedAt, candidate.OccurrenceCount)
	if err != nil {
		return 0, err
	}
	if err := commit(); err != nil {
		return 0, err
	}
	return sourceID, nil
}

func (r *ExploreRegistryRepository) RecordSuccess(providerID int, syncedAt time.Time, etag, lastModified string) error {
	result, err := r.db.Exec(`UPDATE explore_registry_providers SET etag=NULLIF($3,''), last_modified=NULLIF($4,''), last_sync_at=$2, last_success_at=$2, consecutive_failures=0, last_error=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=$1`, providerID, syncedAt, etag, lastModified)
	return expectProviderUpdate(result, err, providerID)
}

func (r *ExploreRegistryRepository) RecordFailure(providerID int, syncedAt time.Time, cause error) error {
	result, err := r.db.Exec(`UPDATE explore_registry_providers SET last_sync_at=$2, consecutive_failures=consecutive_failures+1, last_error=$3, updated_at=CURRENT_TIMESTAMP WHERE id=$1`, providerID, syncedAt, ClipExploreError(cause))
	return expectProviderUpdate(result, err, providerID)
}

func expectProviderUpdate(result sql.Result, err error, providerID int) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("explore registry provider %d not found", providerID)
	}
	return nil
}

var _ explore.RegistryStore = (*ExploreRegistryRepository)(nil)

// ExploreRegistryQueue adapts the existing idempotent queue repository to the
// narrow registry queue interface without changing Task1's public API.
type ExploreRegistryQueue struct{ queue *ExploreQueueRepository }

func NewExploreRegistryQueue(queue *ExploreQueueRepository) ExploreRegistryQueue {
	return ExploreRegistryQueue{queue: queue}
}

func (q ExploreRegistryQueue) Enqueue(sourceID int, taskType string, priority int) error {
	if q.queue == nil {
		return errors.New("explore queue is required")
	}
	_, err := q.queue.Enqueue(sourceID, taskType, priority)
	return err
}

var _ explore.RegistryQueue = ExploreRegistryQueue{}
