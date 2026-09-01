package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/bytedance/rss-pal/internal/util"
	"github.com/lib/pq"
)

const exploreRegistryCandidateUpsertSQL = `
	INSERT INTO recommended_feeds (url, title, category, language, feed_type, site_url, normalized_url, validation_status, first_discovered_at, last_observed_at)
	VALUES ($1, $2, $3, 'en', 'rss', NULLIF($4, ''), $1, 'pending', $5, $5)
	ON CONFLICT (normalized_url) DO UPDATE SET
		title = CASE WHEN NULLIF(EXCLUDED.title, '') IS NULL THEN recommended_feeds.title ELSE EXCLUDED.title END,
		site_url = COALESCE(NULLIF(EXCLUDED.site_url, ''), recommended_feeds.site_url),
		category = COALESCE(NULLIF(EXCLUDED.category, ''), recommended_feeds.category),
		last_observed_at = EXCLUDED.last_observed_at
	RETURNING id`

const ExploreRelatedSeedsSQL = `
	WITH raw_seeds AS (
		SELECT COALESCE(feed.owner_id,0) AS owner_key,
		       COALESCE(source.site_url,feed.url) AS url,
		       COALESCE(feed.last_fetched_at,feed.created_at) AS seed_at
		FROM feeds feed
		LEFT JOIN recommended_feeds source
		  ON source.normalized_url=lower(btrim(feed.url)) AND source.merged_into_source_id IS NULL
		WHERE feed.status='active' AND feed.is_active
		UNION ALL
		SELECT COALESCE(feed.owner_id,0),article.url,
		       COALESCE(article.published_at,article.fetched_at)
		FROM articles article JOIN feeds feed ON feed.id=article.feed_id
		WHERE feed.status='active' AND feed.is_active
		  AND COALESCE(article.published_at,article.fetched_at) >= $1 - INTERVAL '30 days'
	)
	SELECT owner_key,url,seed_at FROM raw_seeds
	WHERE url IS NOT NULL AND btrim(url) <> ''
	ORDER BY owner_key,seed_at DESC,url`

const (
	exploreRelatedSeedsPerOwner = 10
	ExploreRelatedSeedWindow    = 6 * time.Hour
)

// ExploreRelatedSeed is a privacy-minimized scheduling input. OwnerKey is
// used only to share the global discovery budget fairly and is never exposed
// downstream or persisted in the public candidate pool.
type ExploreRelatedSeed struct {
	OwnerKey int
	URL      string
	SeedAt   time.Time
}

// SelectExploreRelatedSeeds canonicalizes and de-duplicates inside each owner
// before applying the per-owner quota. It then visits owners round-robin from
// a deterministic rotating window offset, so N owners are all reached within
// ceil(N/limit) continuously scheduled windows when each has one seed.
func SelectExploreRelatedSeeds(raw []ExploreRelatedSeed, now time.Time, limit int) []string {
	ordered := append([]ExploreRelatedSeed(nil), raw...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].OwnerKey != ordered[j].OwnerKey {
			return ordered[i].OwnerKey < ordered[j].OwnerKey
		}
		if !ordered[i].SeedAt.Equal(ordered[j].SeedAt) {
			return ordered[i].SeedAt.After(ordered[j].SeedAt)
		}
		return ordered[i].URL < ordered[j].URL
	})
	collector := newExploreRelatedSeedCollector()
	for _, seed := range ordered {
		collector.Add(seed)
	}
	return collector.Select(now, limit)
}

type exploreRelatedOwnerSeeds struct {
	owner int
	urls  []string
}

// exploreRelatedSeedCollector consumes rows ordered by owner/time. Once ten
// canonical unique URLs have been retained for an owner it keeps scanning but
// does not retain more rows, bounding memory without pre-canonicalization SQL
// sampling that could starve an older unique URL.
type exploreRelatedSeedCollector struct {
	started      bool
	currentOwner int
	currentURLs  []string
	currentSeen  map[string]struct{}
	owners       []exploreRelatedOwnerSeeds
}

func newExploreRelatedSeedCollector() *exploreRelatedSeedCollector {
	return &exploreRelatedSeedCollector{}
}

func (collector *exploreRelatedSeedCollector) Add(seed ExploreRelatedSeed) {
	if !collector.started || seed.OwnerKey != collector.currentOwner {
		collector.flush()
		collector.started = true
		collector.currentOwner = seed.OwnerKey
		collector.currentURLs = nil
		collector.currentSeen = make(map[string]struct{}, exploreRelatedSeedsPerOwner)
	}
	if len(collector.currentURLs) >= exploreRelatedSeedsPerOwner {
		return
	}
	canonical, ok := canonicalExploreRelatedSeedURL(seed.URL)
	if !ok {
		return
	}
	if _, duplicate := collector.currentSeen[canonical]; duplicate {
		return
	}
	collector.currentSeen[canonical] = struct{}{}
	collector.currentURLs = append(collector.currentURLs, canonical)
}

func (collector *exploreRelatedSeedCollector) flush() {
	if !collector.started || len(collector.currentURLs) == 0 {
		return
	}
	collector.owners = append(collector.owners, exploreRelatedOwnerSeeds{
		owner: collector.currentOwner,
		urls:  append([]string(nil), collector.currentURLs...),
	})
}

func (collector *exploreRelatedSeedCollector) Select(now time.Time, limit int) []string {
	collector.flush()
	collector.started = false
	if limit <= 0 || len(collector.owners) == 0 {
		return []string{}
	}
	if limit > explore.MaxRelatedSeeds {
		limit = explore.MaxRelatedSeeds
	}
	window := now.UTC().Unix() / int64(ExploreRelatedSeedWindow/time.Second)
	start := int(((window % int64(len(collector.owners))) * int64(limit)) % int64(len(collector.owners)))
	selected := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for ownerRank := 0; ownerRank < exploreRelatedSeedsPerOwner && len(selected) < limit; ownerRank++ {
		for offset := 0; offset < len(collector.owners) && len(selected) < limit; offset++ {
			owner := collector.owners[(start+offset)%len(collector.owners)]
			if ownerRank >= len(owner.urls) {
				continue
			}
			seed := owner.urls[ownerRank]
			if _, duplicate := seen[seed]; duplicate {
				continue
			}
			seen[seed] = struct{}{}
			selected = append(selected, seed)
		}
	}
	return selected
}

func canonicalExploreRelatedSeedURL(raw string) (string, bool) {
	canonical := util.NormalizeURL(strings.TrimSpace(raw))
	parsed, err := url.Parse(canonical)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return "", false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	return parsed.String(), true
}

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
		WHERE enabled AND provider_kind <> 'related_site'
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

func (r *ExploreRegistryRepository) LoadRelatedProvider(now time.Time) (*explore.RegistryProvider, error) {
	var provider explore.RegistryProvider
	var interval int
	var topic, etag, modified sql.NullString
	err := r.db.QueryRow(`
		SELECT id, provider_key, provider_kind, endpoint, topic, sync_interval_minutes,
		       enabled, etag, last_modified, last_sync_at, last_success_at, consecutive_failures
		FROM explore_registry_providers
		WHERE provider_key='related-sites' AND provider_kind='related_site' AND enabled
		  AND (last_sync_at IS NULL OR last_sync_at + sync_interval_minutes * POWER(2, LEAST(consecutive_failures, 6)) * INTERVAL '1 minute' <= $1)
	`, now).Scan(&provider.ID, &provider.Key, &provider.Kind, &provider.Endpoint, &topic, &interval,
		&provider.Enabled, &etag, &modified, &provider.LastSyncAt, &provider.LastSuccessAt, &provider.ConsecutiveFailures)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	provider.Topic, provider.ETag, provider.LastModified = topic.String, etag.String, modified.String
	provider.SyncInterval = time.Duration(interval) * time.Minute
	return &provider, nil
}

// LoadRelatedSeeds projects only public URLs from visible formal subscription
// data. It never returns a user ID or article ID.
func (r *ExploreRegistryRepository) LoadRelatedSeeds(ctx context.Context, since time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	if limit > explore.MaxRelatedSeeds {
		limit = explore.MaxRelatedSeeds
	}
	rows, err := r.db.QueryContext(ctx, ExploreRelatedSeedsSQL, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	collector := newExploreRelatedSeedCollector()
	for rows.Next() {
		var seed ExploreRelatedSeed
		if err := rows.Scan(&seed.OwnerKey, &seed.URL, &seed.SeedAt); err != nil {
			return nil, err
		}
		collector.Add(seed)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return collector.Select(since, limit), nil
}

func (r *ExploreRegistryRepository) UpsertCandidate(providerID int, candidate explore.Candidate, observedAt time.Time) (int, error) {
	if providerID <= 0 || candidate.ExternalKey == "" || candidate.FeedURL == "" {
		return 0, errors.New("provider, external key, and feed URL are required")
	}
	if candidate.OccurrenceCount < 1 {
		candidate.OccurrenceCount = 1
	}
	title := candidate.Title
	if title == "" {
		title = explore.ClipCandidateTitle(candidate.FeedURL)
	}
	candidate.Title = title
	category := candidate.Topic
	if category == "" {
		category = "general"
	}
	candidate.Topic = category
	if err := explore.ValidateCandidate(candidate); err != nil {
		return 0, fmt.Errorf("invalid explore candidate: %w", err)
	}
	q, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return 0, err
	}
	defer rollback()
	if err := lockExploreCanonicalURL(q, candidate.FeedURL); err != nil {
		return 0, err
	}
	var sourceID int
	err = q.QueryRow(exploreRegistryCandidateUpsertSQL, candidate.FeedURL, title, category, candidate.SiteURL, observedAt).Scan(&sourceID)
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
var _ explore.RelatedSiteSyncStore = (*ExploreRegistryRepository)(nil)

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
