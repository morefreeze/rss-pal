package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	explorelogic "github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/util"
	"github.com/lib/pq"
)

const exploreCandidateInputLimit = 2000

const exploreCandidateSQL = `
	SELECT source.id,source.title,source.category,COALESCE(source.site_url,source.url),
	       source.validation_status,source.is_broken,source.merged_into_source_id,
	       COALESCE(source.health_score,0)
	FROM recommended_feeds source
	WHERE source.validation_status='valid' AND source.is_broken=false
	  AND source.merged_into_source_id IS NULL
	  AND EXISTS (
	      SELECT 1 FROM explore_source_observations observation
	      JOIN explore_registry_providers provider ON provider.id=observation.provider_id
	      WHERE observation.source_id=source.id AND provider.enabled
	        AND observation.last_seen_at >= $1 - GREATEST(provider.sync_interval_minutes * 2 * INTERVAL '1 minute', INTERVAL '6 hours')
	  )
	ORDER BY COALESCE(source.health_score,0) DESC, source.last_observed_at DESC NULLS LAST, source.id
	LIMIT $2`

func newProductionExploreCycle(db *sql.DB, cfg *config.Config) *exploreCycle {
	queue := newSQLExploreQueue(db)
	baseRegistry := &explorelogic.Registry{
		Store:    repository.NewExploreRegistryRepository(db),
		Queue:    repository.NewExploreRegistryQueue(queue.repo),
		Client:   explorelogic.NewProviderClient(cfg.RSSHub.BaseURL),
		Adapters: explorelogic.DefaultProviderAdapters(),
	}
	registry := &scheduledExploreRegistry{
		registry: baseRegistry,
		catalog:  repository.NewExploreCatalogRepository(db),
		queue:    queue.repo,
		related:  repository.NewExploreRelatedTaskRepository(db),
	}
	inputs := &sqlExploreRankInputs{db: db}
	snapshots := &exploreSnapshotCoordinator{
		users:    inputs,
		profiles: inputs,
		store:    repository.NewExploreSnapshotRepository(db),
		logger:   log.Default(),
	}
	return newExploreCycle(exploreCycleDeps{
		registry: registry,
		queue:    queue,
		taskHandler: &exploreTaskRouter{
			source:  repository.NewExploreTaskProcessor(db, explorelogic.NewSourceFetcher(), time.Now),
			related: repository.NewExploreRelatedTaskProcessor(db, explorelogic.NewProviderClient(cfg.RSSHub.BaseURL)),
		},
		snapshots:        snapshots,
		cleanup:          repository.NewExploreSnapshotRepository(db),
		batchLimit:       cfg.Explore.FetchBatchLimit,
		fetchConcurrency: cfg.Explore.FetchConcurrency,
		leaseDuration:    exploreDefaultLease,
		logger:           log.Default(),
	})
}

type sqlExploreQueue struct {
	db   *sql.DB
	repo *repository.ExploreQueueRepository
}

type exploreDueSourceCatalog interface {
	ListDueSources(time.Time, time.Time, time.Time, int) ([]model.ExploreSource, error)
}

type exploreQueueEnqueuer interface {
	Enqueue(int, string, int) (*repository.ExploreQueueTask, error)
}

// scheduledExploreRegistry keeps source refresh scheduling independent from
// provider health. Even when a provider download fails, already-observed due
// sources continue to enter the durable queue for validation or refresh.
type scheduledExploreRegistry struct {
	registry exploreRegistrySyncer
	catalog  exploreDueSourceCatalog
	queue    exploreQueueEnqueuer
	related  interface {
		Produce(context.Context, time.Time, int) (repository.ExploreRelatedProduceResult, error)
	}
}

func (scheduler *scheduledExploreRegistry) SyncDue(ctx context.Context, now time.Time) ([]explorelogic.ProviderSyncResult, error) {
	results, syncErr := scheduler.registry.SyncDue(ctx, now)
	if scheduler.related != nil {
		_, relatedErr := scheduler.related.Produce(ctx, now, 250)
		syncErr = errors.Join(syncErr, relatedErr)
	}
	due, dueErr := scheduler.catalog.ListDueSources(now.Add(-30*time.Minute), now.Add(-3*time.Hour), now.Add(-24*time.Hour), exploreMaxBatchLimit)
	var enqueueErr error
	for _, source := range due {
		taskType, priority := repository.ExploreTaskValidateSource, repository.ExplorePriorityStructuredProvider
		if source.ValidationStatus == model.ExploreValidationValid {
			taskType, priority = repository.ExploreTaskRefreshArticles, repository.ExplorePriorityRefresh
		}
		if source.IsBroken {
			priority = repository.ExplorePriorityBrokenHealthCheck
		}
		if _, err := scheduler.queue.Enqueue(source.ID, taskType, priority); err != nil {
			enqueueErr = errors.Join(enqueueErr, err)
		}
	}
	return results, errors.Join(syncErr, dueErr, enqueueErr)
}

type exploreTaskRouter struct {
	source  exploreTaskHandler
	related exploreTaskHandler
}

func (router *exploreTaskRouter) Process(ctx context.Context, task repository.ExploreQueueTask) error {
	if task.QueueKind == repository.ExploreQueueKindRelated || task.TaskType == repository.ExploreTaskDiscoverRelated {
		if router.related == nil {
			return errors.New("explore related task handler is required")
		}
		return router.related.Process(ctx, task)
	}
	if router.source == nil {
		return errors.New("explore source task handler is required")
	}
	return router.source.Process(ctx, task)
}

func newSQLExploreQueue(db *sql.DB) *sqlExploreQueue {
	return &sqlExploreQueue{db: db, repo: repository.NewExploreQueueRepository(db)}
}

func (queue *sqlExploreQueue) ClaimRun(window time.Time, owner string, lease time.Duration, limit int) (*repository.ExploreFetchRun, []repository.ExploreQueueTask, error) {
	return queue.repo.ClaimRun(window, owner, lease, limit)
}

func (queue *sqlExploreQueue) RecoverExpired(owner string, lease time.Duration) (*repository.ExploreFetchRun, []repository.ExploreQueueTask, error) {
	return queue.repo.RecoverExpired(owner, lease)
}

func (queue *sqlExploreQueue) FinishRun(runID int, cause error) error {
	status, message := model.ExploreFetchRunDone, any(nil)
	if cause != nil {
		status, message = model.ExploreFetchRunFailed, repository.ClipExploreError(cause)
	}
	result, err := queue.db.Exec(`
		UPDATE explore_fetch_runs
		SET status=$2, error_message=$3, completed_at=CURRENT_TIMESTAMP
		WHERE id=$1 AND status='running'`, runID, status, message)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("explore fetch run %d is not running", runID)
	}
	return nil
}

type sqlExploreRankInputs struct{ db *sql.DB }

func (inputs *sqlExploreRankInputs) ListUserIDs(ctx context.Context) ([]int, error) {
	rows, err := inputs.db.QueryContext(ctx, `SELECT id FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (inputs *sqlExploreRankInputs) LoadProfile(ctx context.Context, userID int, now time.Time) (explorelogic.ProfileInput, error) {
	profile, err := repository.NewExploreProfileSignalRepository(inputs.db).Load(ctx, userID, now)
	if err != nil {
		return profile, err
	}
	rows, err := inputs.db.QueryContext(ctx, `
		SELECT article.title, preference.signal_type, preference.created_at
		FROM user_preferences preference JOIN articles article ON article.id=preference.article_id
		WHERE preference.user_id=$1 AND preference.signal_type IN ('save','like')
		  AND preference.created_at >= $2
		ORDER BY preference.created_at DESC, preference.id DESC LIMIT 100`, userID, now.Add(-30*24*time.Hour))
	if err != nil {
		return profile, err
	}
	for rows.Next() {
		var item explorelogic.FormalArticleBehaviorInput
		if err := rows.Scan(&item.Title, &item.SignalType, &item.OccurredAt); err != nil {
			rows.Close()
			return profile, err
		}
		profile.FormalArticleBehaviors = append(profile.FormalArticleBehaviors, item)
	}
	if err := closeExploreRows(rows); err != nil {
		return profile, err
	}

	rows, err = inputs.db.QueryContext(ctx, `
		SELECT article.title, progress.last_read_at
		FROM reading_progress progress JOIN articles article ON article.id=progress.article_id
		WHERE progress.user_id=$1 AND progress.last_read_at >= $2
		ORDER BY progress.last_read_at DESC, article.id DESC LIMIT 100`, userID, now.Add(-30*24*time.Hour))
	if err != nil {
		return profile, err
	}
	for rows.Next() {
		var item explorelogic.FormalArticleBehaviorInput
		if err := rows.Scan(&item.Title, &item.OccurredAt); err != nil {
			rows.Close()
			return profile, err
		}
		item.SignalType = explorelogic.FormalArticleRead
		profile.FormalArticleBehaviors = append(profile.FormalArticleBehaviors, item)
	}
	if err := closeExploreRows(rows); err != nil {
		return profile, err
	}

	rows, err = inputs.db.QueryContext(ctx, `
		SELECT COALESCE(batch_source.source_id,0), COALESCE(batch_source.topic,''), event.event_type, event.occurred_at
		FROM explore_article_events event
		LEFT JOIN explore_articles article ON article.id=event.explore_article_id
		LEFT JOIN LATERAL (
			SELECT source.source_id,source.topic FROM explore_batch_sources source
			JOIN explore_batches batch ON batch.id=source.batch_id AND batch.user_id=source.user_id
			WHERE source.user_id=event.user_id AND source.source_id=article.source_id AND batch.status='done'
			ORDER BY batch.slot_at DESC, source.id DESC LIMIT 1
		) batch_source ON true
		WHERE event.user_id=$1 AND event.occurred_at >= $2
		ORDER BY event.occurred_at DESC, event.id DESC LIMIT 100`, userID, now.Add(-30*24*time.Hour))
	if err != nil {
		return profile, err
	}
	for rows.Next() {
		var item explorelogic.ExploreEventSignalInput
		if err := rows.Scan(&item.SourceID, &item.Topic, &item.EventType, &item.OccurredAt); err != nil {
			rows.Close()
			return profile, err
		}
		profile.ExploreEvents = append(profile.ExploreEvents, item)
	}
	if err := closeExploreRows(rows); err != nil {
		return profile, err
	}

	rows, err = inputs.db.QueryContext(ctx, `SELECT COALESCE(source_id,0), COALESCE(topic,''), feedback_type FROM explore_feedback WHERE user_id=$1 ORDER BY id`, userID)
	if err != nil {
		return profile, err
	}
	defer rows.Close()
	for rows.Next() {
		var item explorelogic.ExplicitFeedbackInput
		if err := rows.Scan(&item.SourceID, &item.Topic, &item.Type); err != nil {
			return profile, err
		}
		profile.Feedback = append(profile.Feedback, item)
	}
	return profile, rows.Err()
}

func (inputs *sqlExploreRankInputs) LoadCandidates(ctx context.Context, now time.Time) ([]explorelogic.RankCandidate, error) {
	rows, err := inputs.db.QueryContext(ctx, exploreCandidateSQL, now, exploreCandidateInputLimit)
	if err != nil {
		return nil, err
	}
	candidates := []explorelogic.RankCandidate{}
	ids := []int{}
	byID := map[int]int{}
	for rows.Next() {
		var candidate explorelogic.RankCandidate
		var rawURL string
		if err := rows.Scan(&candidate.SourceID, &candidate.Title, &candidate.Category, &rawURL, &candidate.ValidationStatus, &candidate.IsBroken, &candidate.MergedIntoSourceID, &candidate.HealthScore); err != nil {
			rows.Close()
			return nil, err
		}
		candidate.Domain = exploreURLDomain(rawURL)
		candidate.Topic = candidate.Category
		byID[candidate.SourceID] = len(candidates)
		ids = append(ids, candidate.SourceID)
		candidates = append(candidates, candidate)
	}
	if err := closeExploreRows(rows); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return candidates, nil
	}

	rows, err = inputs.db.QueryContext(ctx, `
		SELECT observation.source_id,provider.provider_key,COALESCE(provider.topic,''),observation.provider_tags,observation.last_seen_at
		FROM explore_source_observations observation
		JOIN explore_registry_providers provider ON provider.id=observation.provider_id
		WHERE observation.source_id=ANY($1) AND provider.enabled
		  AND observation.last_seen_at >= $2 - GREATEST(provider.sync_interval_minutes * 2 * INTERVAL '1 minute', INTERVAL '6 hours')
		ORDER BY observation.source_id, observation.last_seen_at DESC, observation.id`, pq.Array(ids), now)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sourceID int
		var observation explorelogic.RankObservation
		if err := rows.Scan(&sourceID, &observation.Provider, &observation.Topic, pq.Array(&observation.Tags), &observation.LastObservedAt); err != nil {
			rows.Close()
			return nil, err
		}
		index := byID[sourceID]
		candidates[index].Observations = append(candidates[index].Observations, observation)
		if candidates[index].Provider == "" {
			candidates[index].Provider = observation.Provider
		}
	}
	if err := closeExploreRows(rows); err != nil {
		return nil, err
	}

	rows, err = inputs.db.QueryContext(ctx, `
		SELECT article.source_id,article.id,article.title,article.published_at,article.fetched_at
		FROM recommended_feeds source
		JOIN LATERAL (
			SELECT id,source_id,title,published_at,fetched_at FROM explore_articles
			WHERE source_id=source.id
			ORDER BY COALESCE(published_at,fetched_at) DESC,fetched_at DESC,id DESC LIMIT 5
		) article ON true
		WHERE source.id=ANY($1)
		ORDER BY article.source_id,COALESCE(article.published_at,article.fetched_at) DESC,article.id DESC`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID int
		var article explorelogic.RankArticle
		var published sql.NullTime
		if err := rows.Scan(&sourceID, &article.ID, &article.Title, &published, &article.FetchedAt); err != nil {
			return nil, err
		}
		if published.Valid {
			article.PublishedAt = published.Time
		}
		candidates[byID[sourceID]].Articles = append(candidates[byID[sourceID]].Articles, article)
	}
	return candidates, rows.Err()
}

func closeExploreRows(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	return rows.Close()
}

func exploreURLDomain(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func normalizeExploreFeedURL(raw string) string {
	return util.NormalizeURL(strings.TrimSpace(raw))
}
