package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
)

// ExploreSourceFetcher is deliberately narrower than SourceFetcher so task
// processing tests never need a real network transport.
type ExploreSourceFetcher interface {
	Fetch(context.Context, explore.SourceFetchRequest) (explore.SourceFetchResult, error)
}

// ExploreTaskProcessor applies one already-leased task. Claiming, run
// finalization, concurrency limits, and periodic scheduling belong to the
// worker coordinator rather than this transaction boundary.
type ExploreTaskProcessor struct {
	db      *sql.DB
	fetcher ExploreSourceFetcher
	now     func() time.Time
}

func NewExploreTaskProcessor(db *sql.DB, fetcher ExploreSourceFetcher, now func() time.Time) *ExploreTaskProcessor {
	if fetcher == nil {
		fetcher = explore.NewSourceFetcher()
	}
	if now == nil {
		now = time.Now
	}
	return &ExploreTaskProcessor{db: db, fetcher: fetcher, now: now}
}

func (p *ExploreTaskProcessor) Process(ctx context.Context, task ExploreQueueTask, owner string) error {
	if p == nil || p.db == nil {
		return errors.New("explore task processor requires a database")
	}
	if task.RunID == nil || *task.RunID <= 0 {
		return fmt.Errorf("%w: task %d has no run", ErrExploreLeaseNotHeld, task.ID)
	}
	checkedAt := p.now()
	catalog := NewExploreCatalogRepository(p.db)
	source, err := catalog.GetSourceWithObservations(task.SourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p.finishMissingSource(ctx, task, owner, err)
		}
		return err
	}
	decision := decideExploreTaskNetwork(task.TaskType, source.Source.ValidationStatus)
	if decision != exploreTaskFetch {
		return p.finishWithoutFetch(ctx, task, owner)
	}

	// checkedAt and the complete request are captured before the potentially
	// slow network call. No database transaction is held while Fetch waits.
	result, fetchErr := p.fetcher.Fetch(ctx, buildExploreSourceFetchRequest(*source, task))
	return p.persistFetchOutcome(ctx, task, owner, checkedAt, result, fetchErr)
}

func (p *ExploreTaskProcessor) finishMissingSource(ctx context.Context, task ExploreQueueTask, owner string, cause error) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := NewExploreQueueRepository(p.db).WithQuerier(tx).Invalidate(task.ID, *task.RunID, owner, cause); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *ExploreTaskProcessor) finishWithoutFetch(ctx context.Context, task ExploreQueueTask, owner string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	catalog := NewExploreCatalogRepository(tx)
	queue := NewExploreQueueRepository(p.db).WithQuerier(tx)
	source, err := catalog.GetSource(task.SourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := queue.Invalidate(task.ID, *task.RunID, owner, err); err != nil {
				return err
			}
			return tx.Commit()
		}
		return err
	}
	decision := decideExploreTaskNetwork(task.TaskType, source.ValidationStatus)
	if decision == exploreTaskSkipValidated {
		if _, err := queue.Enqueue(task.SourceID, ExploreTaskRefreshArticles, ExplorePriorityRefresh); err != nil {
			return err
		}
		if err := queue.Complete(task.ID, *task.RunID, owner); err != nil {
			return err
		}
		return tx.Commit()
	}
	if decision == exploreTaskInvalidateWithoutFetch {
		if err := queue.Invalidate(task.ID, *task.RunID, owner, fmt.Errorf("explore %s task is not eligible for source status %s", task.TaskType, source.ValidationStatus)); err != nil {
			return err
		}
		return tx.Commit()
	}
	// State changed between the pool read and the transaction. Leave the lease
	// intact so a later recovery can process it from a fresh snapshot.
	return fmt.Errorf("explore source %d state changed before task processing", task.SourceID)
}

func (p *ExploreTaskProcessor) persistFetchOutcome(ctx context.Context, task ExploreQueueTask, owner string, checkedAt time.Time, result explore.SourceFetchResult, fetchErr error) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	catalog := NewExploreCatalogRepository(tx)
	queue := NewExploreQueueRepository(p.db).WithQuerier(tx)

	// Re-read inside the write transaction so a validation completed by
	// another worker cannot be overwritten by this earlier network result.
	current, err := catalog.GetSourceWithObservations(task.SourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := queue.Invalidate(task.ID, *task.RunID, owner, err); err != nil {
				return err
			}
			return tx.Commit()
		}
		return err
	}
	decision := decideExploreTaskNetwork(task.TaskType, current.Source.ValidationStatus)
	if decision == exploreTaskSkipValidated {
		if _, err := queue.Enqueue(task.SourceID, ExploreTaskRefreshArticles, ExplorePriorityRefresh); err != nil {
			return err
		}
		if err := queue.Complete(task.ID, *task.RunID, owner); err != nil {
			return err
		}
		return tx.Commit()
	}
	if decision == exploreTaskInvalidateWithoutFetch {
		if err := queue.Invalidate(task.ID, *task.RunID, owner, fmt.Errorf("explore %s task is not eligible for source status %s", task.TaskType, current.Source.ValidationStatus)); err != nil {
			return err
		}
		return tx.Commit()
	}
	if fetchErr == nil && result.NotModified && task.TaskType == ExploreTaskRefreshArticles {
		fetchErr = fmt.Errorf("%w: refresh returned not modified", explore.ErrInactiveSource)
	}

	if fetchErr != nil {
		failureDecision := decideExploreTaskFailure(task.TaskType, task.Attempts, fetchErr)
		if failureDecision == exploreTaskInvalidate && errors.Is(fetchErr, explore.ErrInsufficientSourceConfidence) {
			latest := buildExploreSourceFetchRequest(*current, task)
			if explore.HasSourceConfidence(checkedAt, latest.Evidence, latest.DirectProfile) {
				failureDecision = exploreTaskRetry
			}
		}
		if failureDecision == exploreTaskRetry {
			if err := catalog.RecordFetchFailure(task.SourceID, checkedAt, fetchErr); err != nil {
				return err
			}
			if err := queue.Retry(task.ID, *task.RunID, owner, fetchErr); err != nil {
				return err
			}
		} else {
			if err := catalog.MarkValidationInvalid(task.SourceID, checkedAt, fetchErr); err != nil {
				return err
			}
			if err := queue.Invalidate(task.ID, *task.RunID, owner, fetchErr); err != nil {
				return err
			}
		}
		return tx.Commit()
	}

	if result.NotModified {
		if task.TaskType != ExploreTaskRefreshArticles {
			return persistExploreTerminalResult(tx, catalog, queue, task, owner, checkedAt, errors.New("validation source unexpectedly returned not modified"))
		}
		if err := catalog.RecordFetchNotModified(task.SourceID, checkedAt); err != nil {
			return err
		}
		if err := queue.Complete(task.ID, *task.RunID, owner); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err := validateExploreTaskResult(task.TaskType, result); err != nil {
		if task.TaskType == ExploreTaskRefreshArticles && errors.Is(err, explore.ErrInactiveSource) {
			if recordErr := catalog.RecordFetchFailure(task.SourceID, checkedAt, err); recordErr != nil {
				return recordErr
			}
			if retryErr := queue.Retry(task.ID, *task.RunID, owner, err); retryErr != nil {
				return retryErr
			}
			return tx.Commit()
		}
		return persistExploreTerminalResult(tx, catalog, queue, task, owner, checkedAt, err)
	}

	canonicalID := task.SourceID
	if task.TaskType == ExploreTaskValidateSource {
		canonicalID, _, err = catalog.AdoptDiscoveredFeed(task.SourceID, result.FeedURL)
		if err != nil {
			return err
		}
	}
	articles := result.Articles
	if len(articles) > 50 {
		articles = articles[:50]
	}
	for index := range articles {
		articles[index].SourceID = canonicalID
		if articles[index].FetchedAt.Before(checkedAt) {
			articles[index].FetchedAt = checkedAt
		}
	}
	if err := catalog.UpsertArticles(canonicalID, articles, checkedAt); err != nil {
		return err
	}
	if err := catalog.RecordFetchSuccess(canonicalID, checkedAt, result.ETag, result.LastModified); err != nil {
		return err
	}
	if task.TaskType == ExploreTaskValidateSource {
		if _, err := queue.Enqueue(canonicalID, ExploreTaskRefreshArticles, ExplorePriorityRefresh); err != nil {
			return err
		}
	}
	if err := queue.Complete(task.ID, *task.RunID, owner); err != nil {
		return err
	}
	return tx.Commit()
}

func persistExploreTerminalResult(tx *sql.Tx, catalog *ExploreCatalogRepository, queue *ExploreQueueRepository, task ExploreQueueTask, owner string, checkedAt time.Time, cause error) error {
	if err := catalog.MarkValidationInvalid(task.SourceID, checkedAt, cause); err != nil {
		return err
	}
	if err := queue.Invalidate(task.ID, *task.RunID, owner, cause); err != nil {
		return err
	}
	return tx.Commit()
}

type exploreTaskNetworkDecision string

const (
	exploreTaskFetch                  exploreTaskNetworkDecision = "fetch"
	exploreTaskSkipValidated          exploreTaskNetworkDecision = "skip_validated"
	exploreTaskInvalidateWithoutFetch exploreTaskNetworkDecision = "invalidate_without_fetch"
)

type exploreTaskFailureDecision string

const (
	exploreTaskRetry      exploreTaskFailureDecision = "retry"
	exploreTaskInvalidate exploreTaskFailureDecision = "invalidate"
)

func buildExploreSourceFetchRequest(source ExploreCatalogSource, task ExploreQueueTask) explore.SourceFetchRequest {
	request := explore.SourceFetchRequest{
		URL:           source.Source.URL,
		Mode:          explore.SourceFetchValidate,
		DirectProfile: task.TaskType == ExploreTaskValidateSource && task.Priority >= ExplorePriorityDirectProfile,
		Evidence:      make([]explore.ObservationEvidence, 0, len(source.Observations)),
	}
	if task.TaskType == ExploreTaskRefreshArticles {
		request.Mode = explore.SourceFetchRefresh
	}
	for _, observation := range source.Observations {
		request.Evidence = append(request.Evidence, explore.ObservationEvidence{
			ProviderID:            observation.ProviderID,
			ProviderKind:          observation.ProviderKind,
			Enabled:               observation.ProviderEnabled,
			ProviderLastSuccessAt: observation.ProviderLastSuccessAt,
			LastSeenAt:            observation.LastSeenAt,
			OccurrenceCount:       observation.OccurrenceCount,
		})
	}
	return request
}

func decideExploreTaskNetwork(taskType, sourceStatus string) exploreTaskNetworkDecision {
	switch taskType {
	case ExploreTaskValidateSource:
		if sourceStatus == model.ExploreValidationValid {
			return exploreTaskSkipValidated
		}
		return exploreTaskFetch
	case ExploreTaskRefreshArticles:
		if sourceStatus != model.ExploreValidationValid {
			return exploreTaskInvalidateWithoutFetch
		}
		return exploreTaskFetch
	default:
		return exploreTaskInvalidateWithoutFetch
	}
}

func decideExploreTaskFailure(taskType string, attempts int, err error) exploreTaskFailureDecision {
	if errors.Is(err, explore.ErrInsufficientSourceConfidence) {
		if taskType == ExploreTaskValidateSource && attempts >= 3 {
			return exploreTaskInvalidate
		}
		return exploreTaskRetry
	}
	if explore.ClassifySourceFetchError(err) == explore.SourceFetchRetryable {
		return exploreTaskRetry
	}
	return exploreTaskInvalidate
}

func validateExploreTaskResult(taskType string, result explore.SourceFetchResult) error {
	if result.FeedURL == "" {
		return errors.New("successful source response requires a feed URL")
	}
	if taskType == ExploreTaskValidateSource && len(result.Articles) < 2 {
		return errors.New("successful source validation requires at least two articles")
	}
	if taskType == ExploreTaskRefreshArticles && len(result.Articles) == 0 {
		return fmt.Errorf("%w: successful source refresh requires at least one article", explore.ErrInactiveSource)
	}
	return nil
}
