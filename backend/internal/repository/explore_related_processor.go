package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
)

type ExploreRelatedFetcher interface {
	Fetch(context.Context, string, string, string) (explore.ProviderFetchResult, error)
}

// ExploreRelatedTaskProcessor owns the network boundary for related-site
// discovery. Producer cycles only persist seeds; a leased run task performs
// one seed request and queues any discovered sources for a later run.
type ExploreRelatedTaskProcessor struct {
	db      *sql.DB
	queue   *ExploreQueueRepository
	fetcher ExploreRelatedFetcher
}

func NewExploreRelatedTaskProcessor(db *sql.DB, fetcher ExploreRelatedFetcher) *ExploreRelatedTaskProcessor {
	return &ExploreRelatedTaskProcessor{db: db, queue: NewExploreQueueRepository(db), fetcher: fetcher}
}

func (p *ExploreRelatedTaskProcessor) Process(ctx context.Context, task ExploreQueueTask) error {
	if p == nil || p.db == nil || p.queue == nil || p.fetcher == nil {
		return errors.New("explore related processor dependencies are required")
	}
	if task.QueueKind != ExploreQueueKindRelated || task.TaskType != ExploreTaskDiscoverRelated || task.RunID == nil || task.ProviderID <= 0 || task.SeedURL == "" {
		return fmt.Errorf("invalid related explore task %d", task.ID)
	}
	token, err := exploreTaskLeaseToken(task)
	if err != nil {
		return err
	}
	fetched, err := p.fetcher.Fetch(ctx, task.SeedURL, "", "")
	if err != nil {
		return p.fail(task, token, err)
	}
	candidates, err := (explore.RelatedSiteDiscoverer{}).Discover(task.SeedURL, fetched.Body)
	if err != nil {
		return p.fail(task, token, err)
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var held int
	if err := tx.QueryRowContext(ctx, `SELECT id FROM explore_related_tasks WHERE id=$1 AND run_id=$2 AND status='leased' AND lease_token=$3 AND lease_expires_at>CURRENT_TIMESTAMP FOR UPDATE`, task.ID, *task.RunID, token).Scan(&held); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: related task %d", ErrExploreLeaseLost, task.ID)
		}
		return err
	}
	registry := &ExploreRegistryRepository{db: tx}
	queue := p.queue.WithQuerier(tx)
	var processingErr error
	for _, candidate := range explore.NormalizeCandidates(candidates) {
		sourceID, upsertErr := registry.UpsertCandidate(task.ProviderID, candidate, time.Now())
		if upsertErr != nil {
			processingErr = errors.Join(processingErr, upsertErr)
			continue
		}
		priority := ExplorePriorityRelated
		if candidate.SiteURL != "" && candidate.SiteURL != candidate.FeedURL {
			priority = ExplorePriorityDirectProfile
		}
		if _, enqueueErr := queue.Enqueue(sourceID, ExploreTaskValidateSource, priority); enqueueErr != nil {
			processingErr = errors.Join(processingErr, enqueueErr)
		}
	}
	if processingErr != nil {
		_ = tx.Rollback()
		return p.queue.RetryRelated(task.ID, *task.RunID, token, processingErr)
	}
	if err := queue.CompleteRelated(task.ID, *task.RunID, token); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *ExploreRelatedTaskProcessor) fail(task ExploreQueueTask, token string, cause error) error {
	if task.Attempts >= 3 {
		return p.queue.InvalidateRelated(task.ID, *task.RunID, token, cause)
	}
	return p.queue.RetryRelated(task.ID, *task.RunID, token, cause)
}
