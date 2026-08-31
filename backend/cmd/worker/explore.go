package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	explorelogic "github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
)

const (
	exploreMaxBatchLimit      = 500
	exploreDefaultConcurrency = 5
	exploreDefaultLease       = 20 * time.Minute
	// A source fetch can consume two independent 20s network phases. The last
	// queue position must remain leased while earlier concurrency waves finish.
	exploreTaskWorstCaseDuration = 40 * time.Second
	exploreLeaseSafetyMargin     = 5 * time.Minute
	exploreSnapshotStaleAfter    = 45 * time.Minute
)

type exploreClock interface{ Now() time.Time }
type realExploreClock struct{}

func (realExploreClock) Now() time.Time { return time.Now() }

type exploreRegistrySyncer interface {
	SyncDue(context.Context, time.Time) ([]explorelogic.ProviderSyncResult, error)
}

type exploreQueueDispatcher interface {
	ClaimRun(time.Time, string, time.Duration, int) (*repository.ExploreFetchRun, []repository.ExploreQueueTask, error)
	FinishRun(int, error) error
}

type exploreTaskHandler interface {
	Process(context.Context, repository.ExploreQueueTask, string) error
}

type exploreSnapshotRunner interface {
	GenerateAll(context.Context, time.Time, time.Time) exploreSnapshotGenerationResult
}

type exploreSnapshotGenerationResult struct {
	Done            int
	FailedPersisted int
	Pending         int
	FailWriteErrors int
	TransientErrors int
	NoWork          int
}

func (result exploreSnapshotGenerationResult) NeedsRetry() bool {
	return result.Pending > 0 || result.FailWriteErrors > 0 || result.TransientErrors > 0
}

type exploreCycleDeps struct {
	clock            exploreClock
	registry         exploreRegistrySyncer
	queue            exploreQueueDispatcher
	taskHandler      exploreTaskHandler
	snapshots        exploreSnapshotRunner
	batchLimit       int
	fetchConcurrency int
	leaseDuration    time.Duration
	owner            string
	logger           *log.Logger
}

// exploreCycle only makes lightweight launch decisions. Provider/queue work
// and snapshot work have independent logical-window guards, so a slow queue
// cannot delay a due personalized snapshot.
type exploreCycle struct {
	deps            exploreCycleDeps
	mu              sync.Mutex
	providerWindows map[time.Time]struct{}
	snapshotSlots   map[time.Time]struct{}
}

func newExploreCycle(deps exploreCycleDeps) *exploreCycle {
	if deps.clock == nil {
		deps.clock = realExploreClock{}
	}
	if deps.batchLimit <= 0 || deps.batchLimit > exploreMaxBatchLimit {
		deps.batchLimit = exploreMaxBatchLimit
	}
	if deps.fetchConcurrency <= 0 {
		deps.fetchConcurrency = exploreDefaultConcurrency
	}
	if deps.leaseDuration <= 0 {
		deps.leaseDuration = exploreDefaultLease
	}
	if required := requiredExploreLeaseDuration(deps.batchLimit, deps.fetchConcurrency); deps.leaseDuration < required {
		deps.leaseDuration = required
	}
	if deps.owner == "" {
		deps.owner = newExploreWorkerOwner()
	}
	if deps.logger == nil {
		deps.logger = log.Default()
	}
	return &exploreCycle{
		deps:            deps,
		providerWindows: make(map[time.Time]struct{}),
		snapshotSlots:   make(map[time.Time]struct{}),
	}
}

func requiredExploreLeaseDuration(batchLimit, concurrency int) time.Duration {
	if batchLimit <= 0 || batchLimit > exploreMaxBatchLimit {
		batchLimit = exploreMaxBatchLimit
	}
	if concurrency <= 0 {
		concurrency = exploreDefaultConcurrency
	}
	waves := (batchLimit + concurrency - 1) / concurrency
	return time.Duration(waves)*exploreTaskWorstCaseDuration + exploreLeaseSafetyMargin
}

func newExploreWorkerOwner() string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err == nil {
		return fmt.Sprintf("worker-%d-%s", os.Getpid(), hex.EncodeToString(random))
	}
	return fmt.Sprintf("worker-%d-%d", os.Getpid(), time.Now().UnixNano())
}

func (cycle *exploreCycle) Run(ctx context.Context) {
	if cycle == nil {
		return
	}
	now := cycle.deps.clock.Now()
	schedule := explorelogic.ExploreScheduleAt(now)
	// Launch the current slot before provider work. Snapshot ranking consumes
	// the last validated cache and deliberately never waits for queue drain.
	if schedule.HasCurrent && cycle.deps.snapshots != nil && cycle.markSnapshotSlotStarted(schedule.SlotAt) {
		go func() {
			if cycle.deps.snapshots.GenerateAll(ctx, schedule.SlotAt, now).NeedsRetry() {
				cycle.clearSnapshotSlot(schedule.SlotAt)
			}
		}()
	}
	if window, due := dueExploreProviderWindow(schedule, now); due && cycle.markProviderWindowStarted(window) {
		go cycle.runProviderWindow(ctx, window, now)
	}
}

func dueExploreProviderWindow(schedule explorelogic.ExploreSchedule, now time.Time) (time.Time, bool) {
	window := schedule.NextProviderSyncAt
	return window, !window.IsZero() && !now.Before(window) && now.Before(schedule.NextSlotAt)
}

func (cycle *exploreCycle) markProviderWindowStarted(window time.Time) bool {
	cycle.mu.Lock()
	defer cycle.mu.Unlock()
	if _, exists := cycle.providerWindows[window]; exists {
		return false
	}
	cycle.providerWindows[window] = struct{}{}
	return true
}

func (cycle *exploreCycle) markSnapshotSlotStarted(slot time.Time) bool {
	cycle.mu.Lock()
	defer cycle.mu.Unlock()
	if _, exists := cycle.snapshotSlots[slot]; exists {
		return false
	}
	cycle.snapshotSlots[slot] = struct{}{}
	return true
}

func (cycle *exploreCycle) clearSnapshotSlot(slot time.Time) {
	cycle.mu.Lock()
	delete(cycle.snapshotSlots, slot)
	cycle.mu.Unlock()
}

func (cycle *exploreCycle) runProviderWindow(ctx context.Context, window, now time.Time) {
	started := time.Now()
	results, err := cycle.deps.registry.SyncDue(ctx, now)
	if err != nil {
		cycle.deps.logger.Printf("explore provider_sync window=%s error=true duration_ms=%d", window.Format(time.RFC3339), time.Since(started).Milliseconds())
	} else {
		candidates, failures := 0, 0
		for _, result := range results {
			candidates += result.Candidates
			if result.Err != nil {
				failures++
				cycle.deps.logger.Printf("explore provider_sync provider_id=%d error=true", result.ProviderID)
			}
		}
		cycle.deps.logger.Printf("explore provider_sync window=%s providers=%d failures=%d candidates=%d duration_ms=%d", window.Format(time.RFC3339), len(results), failures, candidates, time.Since(started).Milliseconds())
	}
	cycle.processQueueWindow(ctx, window)
}

func (cycle *exploreCycle) processQueueWindow(ctx context.Context, window time.Time) {
	run, tasks, err := cycle.deps.queue.ClaimRun(window, cycle.deps.owner, cycle.deps.leaseDuration, cycle.deps.batchLimit)
	if err != nil {
		cycle.deps.logger.Printf("explore queue_claim window=%s error=true", window.Format(time.RFC3339))
		return
	}
	if run == nil || len(tasks) == 0 {
		return
	}
	started := time.Now()
	jobs := make(chan repository.ExploreQueueTask)
	var workers sync.WaitGroup
	var failures atomic.Int32
	workerCount := cycle.deps.fetchConcurrency
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}
	workers.Add(workerCount)
	for index := 0; index < workerCount; index++ {
		go func() {
			defer workers.Done()
			for task := range jobs {
				if err := cycle.deps.taskHandler.Process(ctx, task, cycle.deps.owner); err != nil {
					failures.Add(1)
					cycle.deps.logger.Printf("explore task run_id=%d task_id=%d source_id=%d error=true", run.ID, task.ID, task.SourceID)
				}
			}
		}()
	}
	for _, task := range tasks {
		select {
		case <-ctx.Done():
			failures.Add(1)
		case jobs <- task:
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()
	failureCount := int(failures.Load())
	var runErr error
	if failureCount > 0 {
		runErr = safeExploreError(fmt.Sprintf("task failures=%d", failureCount))
	}
	if err := cycle.deps.queue.FinishRun(run.ID, runErr); err != nil {
		cycle.deps.logger.Printf("explore queue_finish run_id=%d error=true", run.ID)
	}
	cycle.deps.logger.Printf("explore queue_run run_id=%d claimed=%d failures=%d duration_ms=%d", run.ID, len(tasks), failureCount, time.Since(started).Milliseconds())
}

func startExploreWorker(ctx context.Context, cycle *exploreCycle) {
	cycle.Run(ctx)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cycle.Run(ctx)
			}
		}
	}()
}

type exploreUserLister interface {
	ListUserIDs(context.Context) ([]int, error)
}

type exploreRankInputLoader interface {
	LoadProfile(context.Context, int, time.Time) (explorelogic.ProfileInput, error)
	LoadCandidates(context.Context, time.Time) ([]explorelogic.RankCandidate, error)
}

type exploreSnapshotStore interface {
	Claim(int, time.Time, time.Time, time.Duration) (*repository.ExploreSnapshotClaim, bool, error)
	Publish(int, repository.ExploreSnapshotGenerationToken, []repository.ExploreSnapshotSourceInput) (*model.ExploreBatch, error)
	Fail(int, repository.ExploreSnapshotGenerationToken, error) error
}

type exploreSnapshotCoordinator struct {
	users    exploreUserLister
	profiles exploreRankInputLoader
	store    exploreSnapshotStore
	logger   *log.Logger
}

func (runner *exploreSnapshotCoordinator) GenerateAll(ctx context.Context, slotAt, now time.Time) exploreSnapshotGenerationResult {
	started := time.Now()
	result := exploreSnapshotGenerationResult{}
	userIDs, err := runner.users.ListUserIDs(ctx)
	if err != nil {
		runner.logger.Printf("explore snapshot slot=%s list_users_error=true", slotAt.Format(time.RFC3339))
		result.TransientErrors++
		return result
	}
	candidates, candidateErr := runner.profiles.LoadCandidates(ctx, now)
	for _, userID := range userIDs {
		claim, owned, err := runner.store.Claim(userID, slotAt, now, exploreSnapshotStaleAfter)
		if err != nil {
			result.TransientErrors++
			runner.logger.Printf("explore snapshot user_id=%d slot=%s claim_error=true", userID, slotAt.Format(time.RFC3339))
			continue
		}
		if !owned || claim == nil {
			if claim == nil || claim.Batch.Status == model.ExploreBatchPending {
				result.Pending++
			} else {
				result.NoWork++
			}
			continue
		}
		if candidateErr != nil {
			runner.recordSnapshotFailure(&result, claim, safeExploreError("candidate input unavailable"))
			runner.logger.Printf("explore snapshot batch_id=%d user_id=%d slot=%s input_error=true", claim.Batch.ID, userID, slotAt.Format(time.RFC3339))
			continue
		}
		profileInput, err := runner.profiles.LoadProfile(ctx, userID, now)
		if err != nil {
			runner.recordSnapshotFailure(&result, claim, safeExploreError("profile input unavailable"))
			runner.logger.Printf("explore snapshot batch_id=%d user_id=%d slot=%s profile_error=true", claim.Batch.ID, userID, slotAt.Format(time.RFC3339))
			continue
		}
		ranked := explorelogic.RankExploreCandidates(explorelogic.BuildExploreProfile(profileInput), candidates, now)
		discarded := len(candidates) - len(ranked)
		values := make([]repository.ExploreSnapshotSourceInput, len(ranked))
		for index, value := range ranked {
			values[index] = repository.ExploreSnapshotSourceInput{SourceID: value.SourceID, Score: value.Score, Topic: value.Topic, Reason: value.Reason}
		}
		if _, err := runner.store.Publish(claim.Batch.ID, claim.GenerationToken, values); err != nil {
			runner.recordSnapshotFailure(&result, claim, safeExploreError("snapshot publish failed"))
			runner.logger.Printf("explore snapshot batch_id=%d user_id=%d slot=%s publish_error=true candidates=%d discarded=%d", claim.Batch.ID, userID, slotAt.Format(time.RFC3339), len(values), discarded)
			continue
		}
		result.Done++
		runner.logger.Printf("explore snapshot batch_id=%d user_id=%d slot=%s candidates=%d discarded=%d", claim.Batch.ID, userID, slotAt.Format(time.RFC3339), len(values), discarded)
	}
	runner.logger.Printf("explore snapshot slot=%s users=%d done=%d failed_persisted=%d pending=%d fail_write_errors=%d transient_errors=%d no_work=%d candidates=%d duration_ms=%d", slotAt.Format(time.RFC3339), len(userIDs), result.Done, result.FailedPersisted, result.Pending, result.FailWriteErrors, result.TransientErrors, result.NoWork, len(candidates), time.Since(started).Milliseconds())
	return result
}

func (runner *exploreSnapshotCoordinator) recordSnapshotFailure(result *exploreSnapshotGenerationResult, claim *repository.ExploreSnapshotClaim, cause error) {
	if err := runner.store.Fail(claim.Batch.ID, claim.GenerationToken, cause); err != nil {
		result.FailWriteErrors++
		return
	}
	result.FailedPersisted++
}

type safeExploreError string

func (err safeExploreError) Error() string { return string(err) }
