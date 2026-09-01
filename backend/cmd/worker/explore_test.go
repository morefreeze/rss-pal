package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	explorelogic "github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

var exploreTestShanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

type fakeExploreClock struct{ now time.Time }

func (clock *fakeExploreClock) Now() time.Time { return clock.now }

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (buffer *lockedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.b.Write(value)
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.b.String()
}

type fakeExploreRegistry struct {
	mu      sync.Mutex
	windows []time.Time
	results []explorelogic.ProviderSyncResult
	err     error
}

type fakeExploreDueCatalog struct{ sources []model.ExploreSource }

func (catalog *fakeExploreDueCatalog) ListDueSources(time.Time, time.Time, int) ([]model.ExploreSource, error) {
	return catalog.sources, nil
}

type fakeExploreEnqueuer struct {
	mu    sync.Mutex
	tasks []repository.ExploreQueueTask
}

func (queue *fakeExploreEnqueuer) Enqueue(sourceID int, taskType string, priority int) (*repository.ExploreQueueTask, error) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	task := repository.ExploreQueueTask{SourceID: sourceID, TaskType: taskType, Priority: priority}
	queue.tasks = append(queue.tasks, task)
	return &task, nil
}

func (registry *fakeExploreRegistry) SyncDue(_ context.Context, now time.Time) ([]explorelogic.ProviderSyncResult, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.windows = append(registry.windows, now)
	return registry.results, registry.err
}

func (registry *fakeExploreRegistry) count() int {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return len(registry.windows)
}

type fakeExploreQueue struct {
	mu               sync.Mutex
	recoverCalls     int
	claimCalls       int
	windows          []time.Time
	limits           []int
	leases           []time.Duration
	owners           []string
	tasks            []repository.ExploreQueueTask
	recoveryRun      *repository.ExploreFetchRun
	recoveryTasks    []repository.ExploreQueueTask
	recoveryReturned bool
	dispatchOrder    []string
	finishedRun      []int
}

func (queue *fakeExploreQueue) RecoverExpired(owner string, lease time.Duration) (*repository.ExploreFetchRun, []repository.ExploreQueueTask, error) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.recoverCalls++
	queue.dispatchOrder = append(queue.dispatchOrder, "recover")
	if queue.recoveryRun == nil || queue.recoveryReturned {
		return nil, nil, nil
	}
	queue.recoveryReturned = true
	run := *queue.recoveryRun
	tasks := append([]repository.ExploreQueueTask(nil), queue.recoveryTasks...)
	for index := range tasks {
		tasks[index].RunID = &run.ID
		leaseOwner := owner
		leaseToken := "recovered-token"
		tasks[index].LeaseOwner = &leaseOwner
		tasks[index].LeaseToken = &leaseToken
	}
	queue.leases = append(queue.leases, lease)
	return &run, tasks, nil
}

func (queue *fakeExploreQueue) ClaimRun(window time.Time, owner string, lease time.Duration, limit int) (*repository.ExploreFetchRun, []repository.ExploreQueueTask, error) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.claimCalls++
	queue.dispatchOrder = append(queue.dispatchOrder, "claim")
	queue.windows = append(queue.windows, window)
	queue.limits = append(queue.limits, limit)
	queue.leases = append(queue.leases, lease)
	queue.owners = append(queue.owners, owner)
	runID := 71
	tasks := append([]repository.ExploreQueueTask(nil), queue.tasks...)
	for index := range tasks {
		tasks[index].RunID = &runID
		leaseOwner := owner
		leaseToken := "fresh-token"
		tasks[index].LeaseOwner = &leaseOwner
		tasks[index].LeaseToken = &leaseToken
	}
	return &repository.ExploreFetchRun{ID: runID, WindowAt: window, Status: model.ExploreFetchRunRunning, ClaimedCount: len(tasks)}, tasks, nil
}

func (queue *fakeExploreQueue) FinishRun(runID int, _ error) error {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.finishedRun = append(queue.finishedRun, runID)
	return nil
}

func (queue *fakeExploreQueue) snapshot() (int, []time.Time, []int) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return queue.claimCalls, append([]time.Time(nil), queue.windows...), append([]int(nil), queue.limits...)
}

func (queue *fakeExploreQueue) leaseSnapshot() []time.Duration {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return append([]time.Duration(nil), queue.leases...)
}

func (queue *fakeExploreQueue) finishCount() int {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return len(queue.finishedRun)
}

type fakeExploreTaskHandler struct {
	started chan int
	release chan struct{}
	failID  int
	active  atomic.Int32
	peak    atomic.Int32
	done    atomic.Int32
}

func (handler *fakeExploreTaskHandler) Process(_ context.Context, task repository.ExploreQueueTask) error {
	active := handler.active.Add(1)
	for {
		peak := handler.peak.Load()
		if active <= peak || handler.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	if handler.started != nil {
		handler.started <- task.ID
	}
	if handler.release != nil {
		<-handler.release
	}
	handler.active.Add(-1)
	handler.done.Add(1)
	if task.ID == handler.failID {
		return errors.New("upstream body SECRET-PROFILE")
	}
	return nil
}

type fakeExploreSnapshotRunner struct {
	mu      sync.Mutex
	slots   []time.Time
	started chan time.Time
	release chan struct{}
	retry   bool
}

type fakeExploreCleanup struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (cleanup *fakeExploreCleanup) Cleanup(time.Time) (int64, int64, error) {
	cleanup.mu.Lock()
	defer cleanup.mu.Unlock()
	cleanup.calls++
	return 2, 3, cleanup.err
}

func (cleanup *fakeExploreCleanup) count() int {
	cleanup.mu.Lock()
	defer cleanup.mu.Unlock()
	return cleanup.calls
}

func (runner *fakeExploreSnapshotRunner) GenerateAll(_ context.Context, slotAt, _ time.Time) exploreSnapshotGenerationResult {
	runner.mu.Lock()
	runner.slots = append(runner.slots, slotAt)
	retry := runner.retry
	runner.mu.Unlock()
	if runner.started != nil {
		runner.started <- slotAt
	}
	if runner.release != nil {
		<-runner.release
	}
	if retry {
		return exploreSnapshotGenerationResult{Pending: 1}
	}
	return exploreSnapshotGenerationResult{Done: 1}
}

func (runner *fakeExploreSnapshotRunner) setRetry(retry bool) {
	runner.mu.Lock()
	runner.retry = retry
	runner.mu.Unlock()
}

func TestExploreCycleRetriesFailedSnapshotButKeepsSuccessfulSlotGuard(t *testing.T) {
	now := time.Date(2026, 9, 1, 11, 0, 0, 0, exploreTestShanghai)
	snapshots := &fakeExploreSnapshotRunner{retry: true}
	cycle := newExploreCycleForTest(now, &fakeExploreRegistry{}, &fakeExploreQueue{}, &fakeExploreTaskHandler{}, snapshots, log.New(&bytes.Buffer{}, "", 0))
	cycle.Run(context.Background())
	waitExplore(t, func() bool { return snapshots.count() == 1 })
	waitExplore(t, func() bool { return !exploreSnapshotSlotMarked(cycle, now) })
	cycle.Run(context.Background())
	waitExplore(t, func() bool { return snapshots.count() == 2 })
	waitExplore(t, func() bool { return !exploreSnapshotSlotMarked(cycle, now) })

	snapshots.setRetry(false)
	cycle.Run(context.Background())
	waitExplore(t, func() bool { return snapshots.count() == 3 })
	cycle.Run(context.Background())
	time.Sleep(20 * time.Millisecond)
	if got := snapshots.count(); got != 3 {
		t.Fatalf("successful slot generated %d times, want 3 total calls", got)
	}
}

func exploreSnapshotSlotMarked(cycle *exploreCycle, slot time.Time) bool {
	cycle.mu.Lock()
	defer cycle.mu.Unlock()
	_, exists := cycle.snapshotSlots[slot]
	return exists
}

func (runner *fakeExploreSnapshotRunner) count() int {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return len(runner.slots)
}

func newExploreCycleForTest(now time.Time, registry *fakeExploreRegistry, queue *fakeExploreQueue, handler *fakeExploreTaskHandler, snapshots *fakeExploreSnapshotRunner, logger *log.Logger) *exploreCycle {
	return newExploreCycle(exploreCycleDeps{
		clock:            &fakeExploreClock{now: now},
		registry:         registry,
		queue:            queue,
		taskHandler:      handler,
		snapshots:        snapshots,
		batchLimit:       500,
		fetchConcurrency: 5,
		leaseDuration:    10 * time.Minute,
		owner:            "worker-test",
		logger:           logger,
	})
}

func TestExploreCycleRunsProviderSyncThirtyMinutesBeforeSlotAndClaimsOncePerWindow(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 30, 0, 0, exploreTestShanghai)
	registry := &fakeExploreRegistry{}
	queue := &fakeExploreQueue{}
	cycle := newExploreCycleForTest(now, registry, queue, &fakeExploreTaskHandler{}, &fakeExploreSnapshotRunner{}, log.New(&bytes.Buffer{}, "", 0))

	cycle.Run(context.Background())
	cycle.Run(context.Background())
	waitExplore(t, func() bool { calls, _, _ := queue.snapshot(); return calls == 1 })

	if registry.count() != 1 {
		t.Fatalf("provider sync calls = %d, want 1", registry.count())
	}
	calls, windows, limits := queue.snapshot()
	if calls != 1 || !windows[0].Equal(now) {
		t.Fatalf("queue claims = %d windows=%v, want one at %v", calls, windows, now)
	}
	if limits[0] != 500 {
		t.Fatalf("claim limit = %d, want 500", limits[0])
	}
}

func TestExploreCycleRunsCleanupOncePerShanghaiDayAndRetriesFailureWithoutBlocking(t *testing.T) {
	now := time.Date(2026, 9, 1, 3, 0, 0, 0, exploreTestShanghai)
	cleanup := &fakeExploreCleanup{err: errors.New("db unavailable")}
	snapshots := &fakeExploreSnapshotRunner{}
	var output lockedBuffer
	cycle := newExploreCycle(exploreCycleDeps{
		clock: &fakeExploreClock{now: now}, registry: &fakeExploreRegistry{}, queue: &fakeExploreQueue{},
		taskHandler: &fakeExploreTaskHandler{}, snapshots: snapshots, cleanup: cleanup,
		owner: "worker-test", logger: log.New(&output, "", 0),
	})
	cycle.Run(context.Background())
	waitExplore(t, func() bool { return cleanup.count() == 1 })
	if snapshots.count() != 0 || !strings.Contains(output.String(), "cleanup") {
		t.Fatalf("cleanup failure blocked/log missing: snapshots=%d logs=%s", snapshots.count(), output.String())
	}
	cleanup.mu.Lock()
	cleanup.err = nil
	cleanup.mu.Unlock()
	cycle.Run(context.Background())
	waitExplore(t, func() bool { return cleanup.count() == 2 })
	cycle.Run(context.Background())
	time.Sleep(20 * time.Millisecond)
	if cleanup.count() != 2 {
		t.Fatalf("successful daily cleanup repeated: %d", cleanup.count())
	}
}

func TestExploreCycleRecoversOriginalRunBeforeClaimingFreshWindow(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 30, 0, 0, exploreTestShanghai)
	queue := &fakeExploreQueue{
		recoveryRun:   &repository.ExploreFetchRun{ID: 41, Status: model.ExploreFetchRunFailed, ClaimedCount: 1},
		recoveryTasks: makeExploreTasks(1),
		tasks:         makeExploreTasks(1),
	}
	handler := &fakeExploreTaskHandler{}
	cycle := newExploreCycleForTest(now, &fakeExploreRegistry{}, queue, handler, &fakeExploreSnapshotRunner{}, log.New(&bytes.Buffer{}, "", 0))

	cycle.Run(context.Background())
	waitExplore(t, func() bool { return handler.done.Load() == 2 && queue.finishCount() == 2 })

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.claimCalls != 1 || queue.recoverCalls < 2 {
		t.Fatalf("recover calls=%d claim calls=%d", queue.recoverCalls, queue.claimCalls)
	}
	claimIndex := -1
	for index, operation := range queue.dispatchOrder {
		if operation == "claim" {
			claimIndex = index
			break
		}
	}
	if claimIndex < 1 {
		t.Fatalf("dispatch order=%v, want recovery before fresh claim", queue.dispatchOrder)
	}
	if len(queue.finishedRun) != 2 || queue.finishedRun[0] != 41 || queue.finishedRun[1] != 71 {
		t.Fatalf("finished runs=%v, want original 41 then fresh 71", queue.finishedRun)
	}
}

func TestScheduledExploreRegistryEnqueuesDueValidationAndRefreshDespiteProviderFailure(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 30, 0, 0, exploreTestShanghai)
	base := &fakeExploreRegistry{err: errors.New("provider unavailable")}
	catalog := &fakeExploreDueCatalog{sources: []model.ExploreSource{
		{ID: 4, ValidationStatus: model.ExploreValidationPending},
		{ID: 8, ValidationStatus: model.ExploreValidationValid},
	}}
	queue := &fakeExploreEnqueuer{}
	scheduler := &scheduledExploreRegistry{registry: base, catalog: catalog, queue: queue}
	if _, err := scheduler.SyncDue(context.Background(), now); err == nil {
		t.Fatal("provider failure should remain observable")
	}
	if len(queue.tasks) != 2 {
		t.Fatalf("scheduled tasks = %+v", queue.tasks)
	}
	if queue.tasks[0].SourceID != 4 || queue.tasks[0].TaskType != repository.ExploreTaskValidateSource ||
		queue.tasks[1].SourceID != 8 || queue.tasks[1].TaskType != repository.ExploreTaskRefreshArticles {
		t.Fatalf("scheduled task mapping = %+v", queue.tasks)
	}
}

func TestExploreCycleClampsQueueLimitAndDefaultsConcurrencyToFive(t *testing.T) {
	now := time.Date(2026, 9, 1, 7, 30, 0, 0, exploreTestShanghai)
	queue := &fakeExploreQueue{tasks: makeExploreTasks(12)}
	handler := &fakeExploreTaskHandler{started: make(chan int, 12), release: make(chan struct{})}
	cycle := newExploreCycle(exploreCycleDeps{
		clock: &fakeExploreClock{now: now}, registry: &fakeExploreRegistry{}, queue: queue,
		taskHandler: handler, snapshots: &fakeExploreSnapshotRunner{}, batchLimit: 999,
		owner: "worker-test", logger: log.New(&bytes.Buffer{}, "", 0),
	})

	cycle.Run(context.Background())
	for index := 0; index < 5; index++ {
		select {
		case <-handler.started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for five concurrent tasks")
		}
	}
	select {
	case id := <-handler.started:
		t.Fatalf("sixth task %d started before a concurrency slot was released", id)
	case <-time.After(30 * time.Millisecond):
	}
	close(handler.release)
	waitExplore(t, func() bool { return handler.done.Load() == 12 })

	_, _, limits := queue.snapshot()
	if limits[0] != 500 {
		t.Fatalf("claim limit = %d, want hard clamp 500", limits[0])
	}
	if peak := handler.peak.Load(); peak != 5 {
		t.Fatalf("peak task goroutines = %d, want default concurrency 5", peak)
	}
}

func TestExploreCycleLeaseCoversWorstCaseQueuePosition(t *testing.T) {
	now := time.Date(2026, 9, 1, 7, 30, 0, 0, exploreTestShanghai)
	queue := &fakeExploreQueue{}
	cycle := newExploreCycle(exploreCycleDeps{
		clock: &fakeExploreClock{now: now}, registry: &fakeExploreRegistry{}, queue: queue,
		taskHandler: &fakeExploreTaskHandler{}, snapshots: &fakeExploreSnapshotRunner{},
		batchLimit: 500, fetchConcurrency: 5, leaseDuration: 20 * time.Minute,
		owner: "worker-test", logger: log.New(&bytes.Buffer{}, "", 0),
	})

	cycle.Run(context.Background())
	waitExplore(t, func() bool { return len(queue.leaseSnapshot()) == 1 })
	lease := queue.leaseSnapshot()[0]
	wantTask := 5 * 20 * time.Second // initial HTML plus at most four alternates
	if exploreTaskWorstCaseDuration != wantTask {
		t.Fatalf("task worst case = %v, want %v", exploreTaskWorstCaseDuration, wantTask)
	}
	want := 100*wantTask + exploreLeaseSafetyMargin
	if lease != want || lease != 171*time.Minute+40*time.Second {
		t.Fatalf("lease = %v, want %v", lease, want)
	}
}

func TestExploreCyclePreservesLongerConfiguredLease(t *testing.T) {
	cycle := newExploreCycle(exploreCycleDeps{batchLimit: 10, fetchConcurrency: 5, leaseDuration: 30 * time.Minute})
	if cycle.deps.leaseDuration != 30*time.Minute {
		t.Fatalf("lease = %v, want configured 30m", cycle.deps.leaseDuration)
	}
}

func TestExploreCycleSnapshotDoesNotWaitForQueueDrainAndNightHasNoSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, exploreTestShanghai)
	queue := &fakeExploreQueue{tasks: makeExploreTasks(1)}
	handler := &fakeExploreTaskHandler{started: make(chan int, 1), release: make(chan struct{})}
	snapshots := &fakeExploreSnapshotRunner{started: make(chan time.Time, 1)}
	cycle := newExploreCycleForTest(now, &fakeExploreRegistry{}, queue, handler, snapshots, log.New(&bytes.Buffer{}, "", 0))
	// Seed the provider window as still running to model a slow 07:30 queue.
	cycle.markProviderWindowStarted(now.Add(-30 * time.Minute))
	go cycle.processQueueWindow(context.Background(), now.Add(-30*time.Minute))
	select {
	case <-handler.started:
	case <-time.After(time.Second):
		t.Fatal("queue task did not start")
	}

	cycle.Run(context.Background())
	select {
	case slot := <-snapshots.started:
		if !slot.Equal(now) {
			t.Fatalf("snapshot slot = %v, want %v", slot, now)
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot waited for queue drain")
	}
	close(handler.release)

	night := newExploreCycleForTest(time.Date(2026, 9, 2, 3, 0, 0, 0, exploreTestShanghai), &fakeExploreRegistry{}, &fakeExploreQueue{}, &fakeExploreTaskHandler{}, &fakeExploreSnapshotRunner{}, log.New(&bytes.Buffer{}, "", 0))
	night.Run(context.Background())
	time.Sleep(20 * time.Millisecond)
	if night.deps.snapshots.(*fakeExploreSnapshotRunner).count() != 0 {
		t.Fatal("00:00-08:00 must not generate a snapshot")
	}
}

func TestExploreCycleLogsOnlyIdentifiersAndContinuesAfterFailures(t *testing.T) {
	now := time.Date(2026, 9, 1, 13, 30, 0, 0, exploreTestShanghai)
	var output lockedBuffer
	registry := &fakeExploreRegistry{results: []explorelogic.ProviderSyncResult{
		{ProviderID: 9, ProviderKey: "safe-provider", Err: errors.New("provider body PRIVATE-BODY")},
		{ProviderID: 10, ProviderKey: "next-provider", Candidates: 2},
	}}
	queue := &fakeExploreQueue{tasks: makeExploreTasks(3)}
	handler := &fakeExploreTaskHandler{failID: 2}
	cycle := newExploreCycleForTest(now, registry, queue, handler, &fakeExploreSnapshotRunner{}, log.New(&output, "", 0))

	cycle.Run(context.Background())
	waitExplore(t, func() bool { return handler.done.Load() == 3 && queue.finishCount() == 1 })
	logs := output.String()
	for _, secret := range []string{"PRIVATE-BODY", "SECRET-PROFILE"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("logs leaked private payload %q: %s", secret, logs)
		}
	}
	for _, identifier := range []string{"provider_id=9", "run_id=71", "task_id=2", "source_id=2"} {
		if !strings.Contains(logs, identifier) {
			t.Fatalf("logs missing %q: %s", identifier, logs)
		}
	}
}

func TestExploreSnapshotCoordinatorKeepsLastDoneAfterFailedGeneration(t *testing.T) {
	store := &fakeSnapshotStore{lastDone: 41, claim: &repository.ExploreSnapshotClaim{Batch: model.ExploreBatch{ID: 52, UserID: 7}}}
	runner := &exploreSnapshotCoordinator{
		users: &fakeExploreUsers{ids: []int{7}}, profiles: &fakeExploreRankInputs{profileErr: errors.New("profile unavailable")},
		store: store, logger: log.New(&bytes.Buffer{}, "", 0),
	}
	runner.GenerateAll(context.Background(), time.Date(2026, 9, 1, 8, 0, 0, 0, exploreTestShanghai), time.Now())
	if store.failed != 52 {
		t.Fatalf("failed batch = %d, want 52", store.failed)
	}
	if store.lastDone != 41 {
		t.Fatalf("last done snapshot changed to %d, want 41", store.lastDone)
	}
}

func TestExploreSnapshotCoordinatorReportsFreshUnownedPendingForRetry(t *testing.T) {
	store := &fakeSnapshotStore{
		claim:      &repository.ExploreSnapshotClaim{Batch: model.ExploreBatch{ID: 52, UserID: 7, Status: model.ExploreBatchPending}},
		claimOwned: false,
	}
	runner := &exploreSnapshotCoordinator{
		users: &fakeExploreUsers{ids: []int{7}}, profiles: &fakeExploreRankInputs{},
		store: store, logger: log.New(&bytes.Buffer{}, "", 0),
	}
	result := runner.GenerateAll(context.Background(), time.Date(2026, 9, 1, 8, 0, 0, 0, exploreTestShanghai), time.Now())
	if result.Pending != 1 || !result.NeedsRetry() {
		t.Fatalf("result = %+v, want one retryable pending user", result)
	}
}

func TestExploreSnapshotCoordinatorRetriesPersistedFailure(t *testing.T) {
	store := &fakeSnapshotStore{
		claim:      &repository.ExploreSnapshotClaim{Batch: model.ExploreBatch{ID: 52, UserID: 7, Status: model.ExploreBatchPending}},
		claimOwned: true,
	}
	runner := &exploreSnapshotCoordinator{
		users: &fakeExploreUsers{ids: []int{7}}, profiles: &fakeExploreRankInputs{profileErr: errors.New("profile unavailable")},
		store: store, logger: log.New(&bytes.Buffer{}, "", 0),
	}
	result := runner.GenerateAll(context.Background(), time.Date(2026, 9, 1, 8, 0, 0, 0, exploreTestShanghai), time.Now())
	if result.FailedPersisted != 1 || !result.NeedsRetry() {
		t.Fatalf("result = %+v, want one retryable persisted failure", result)
	}
}

func TestExploreRankInputSQLRequiresFreshEnabledObservation(t *testing.T) {
	normalized := strings.Join(strings.Fields(exploreCandidateSQL), " ")
	for _, fragment := range []string{
		"provider.enabled",
		"observation.last_seen_at >= $1 - GREATEST(provider.sync_interval_minutes * 2 * INTERVAL '1 minute', INTERVAL '6 hours')",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("candidate SQL missing %q: %s", fragment, normalized)
		}
	}
}

func TestExploreProfileSQLLoadsBoundedFormalMetadata(t *testing.T) {
	normalized := strings.Join(strings.Fields(exploreRecentArticleProfileSQL), " ")
	for _, fragment := range []string{
		"article.category", "article.topic", "article.tags", "LEFT(COALESCE(article.content,''),4000)", "LEFT(COALESCE(article.summary_brief,''),1000)",
		"JOIN users profile_user ON profile_user.id=$1",
		"feed.owner_id=$1 AND COALESCE(article.published_at,article.fetched_at) >= $2",
		"feed.owner_id IS NULL AND article.published_at IS NOT NULL",
		"article.published_at >= GREATEST($2,profile_user.shared_visible_from)",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("profile SQL missing %q: %s", fragment, normalized)
		}
	}
	if strings.Count(normalized, "COALESCE(article.published_at,article.fetched_at)") < 2 {
		t.Fatalf("profile SQL does not include recently fetched undated articles: %s", normalized)
	}
}

func TestSQLExploreRankInputsSharedArticlesRespectEachUserVisibilityFloor(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var userA, userB int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash,shared_visible_from) VALUES ('profile-floor-a','x',$1) RETURNING id`, now.Add(-5*24*time.Hour)).Scan(&userA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username,password_hash,shared_visible_from) VALUES ('profile-floor-b','x',$1) RETURNING id`, now.Add(-25*24*time.Hour)).Scan(&userB); err != nil {
		t.Fatal(err)
	}
	var sharedFeed, ownedAFeed, ownedBFeed int
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES ('https://profile-shared.example/feed','shared',NULL) RETURNING id`).Scan(&sharedFeed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES ('https://profile-a.example/feed','owned-a',$1) RETURNING id`, userA).Scan(&ownedAFeed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES ('https://profile-b.example/feed','owned-b',$1) RETURNING id`, userB).Scan(&ownedBFeed); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO articles(feed_id,title,url,published_at,fetched_at) VALUES
			($1,'shared-old','https://profile-shared.example/old',$4,$6),
			($1,'shared-recent','https://profile-shared.example/recent',$5,$6),
			($1,'shared-undated','https://profile-shared.example/undated',NULL,$6),
			($2,'owned-a-undated','https://profile-a.example/undated',NULL,$6),
			($3,'owned-b-recent','https://profile-b.example/recent',$5,$6)`,
		sharedFeed, ownedAFeed, ownedBFeed, now.Add(-20*24*time.Hour), now.Add(-2*24*time.Hour), now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	inputs := &sqlExploreRankInputs{db: db}
	profileA, err := inputs.LoadProfile(context.Background(), userA, now)
	if err != nil {
		t.Fatal(err)
	}
	profileB, err := inputs.LoadProfile(context.Background(), userB, now)
	if err != nil {
		t.Fatal(err)
	}
	assertRecentProfileTitles(t, profileA, []string{"owned-a-undated", "shared-recent"})
	assertRecentProfileTitles(t, profileB, []string{"owned-b-recent", "shared-old", "shared-recent"})
}

func assertRecentProfileTitles(t *testing.T, profile explorelogic.ProfileInput, want []string) {
	t.Helper()
	got := make([]string, 0, len(profile.RecentArticles))
	for _, article := range profile.RecentArticles {
		got = append(got, article.Title)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("recent article titles=%v want=%v", got, want)
	}
}

func TestExploreCycleRetriesPersistedSnapshotFailureThenClosesGuardAfterSuccess(t *testing.T) {
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, exploreTestShanghai)
	slot := explorelogic.ExploreScheduleAt(now).SlotAt
	store := &recoveringSnapshotStore{status: model.ExploreBatchPending}
	runner := &exploreSnapshotCoordinator{
		users:    &fakeExploreUsers{ids: []int{7}},
		profiles: &recoveringRankInputs{now: now},
		store:    store,
		logger:   log.New(&bytes.Buffer{}, "", 0),
	}
	clock := &fakeExploreClock{now: now}
	cycle := newExploreCycle(exploreCycleDeps{
		clock: clock, registry: &fakeExploreRegistry{}, queue: &fakeExploreQueue{},
		taskHandler: &fakeExploreTaskHandler{}, snapshots: runner, owner: "worker-test",
		logger: log.New(&bytes.Buffer{}, "", 0),
	})
	t.Cleanup(func() {
		if t.Failed() {
			status, claims, failures, publishes := store.snapshot()
			t.Logf("final snapshot store state: status=%s claims=%d failures=%d publishes=%d", status, claims, failures, publishes)
		}
	})

	cycle.Run(context.Background())
	waitExplore(t, func() bool {
		status, _, failures, _ := store.snapshot()
		return status == model.ExploreBatchFailed && failures == 1 && !exploreSnapshotSlotMarked(cycle, slot)
	})
	clock.now = now.Add(time.Minute)
	cycle.Run(context.Background())
	waitExplore(t, func() bool {
		status, claims, _, publishes := store.snapshot()
		return status == model.ExploreBatchDone && claims == 2 && publishes == 1 && exploreSnapshotSlotMarked(cycle, slot)
	})
	cycle.Run(context.Background())
	time.Sleep(20 * time.Millisecond)
	if _, claims, _, publishes := store.snapshot(); claims != 2 || publishes != 1 {
		t.Fatalf("closed successful slot ran again: claims=%d publishes=%d", claims, publishes)
	}
}

func TestExploreSnapshotCoordinatorRetriesWhenFailureCannotBePersisted(t *testing.T) {
	store := &fakeSnapshotStore{
		claim:      &repository.ExploreSnapshotClaim{Batch: model.ExploreBatch{ID: 52, UserID: 7, Status: model.ExploreBatchPending}},
		claimOwned: true,
		failErr:    errors.New("database unavailable"),
	}
	runner := &exploreSnapshotCoordinator{
		users: &fakeExploreUsers{ids: []int{7}}, profiles: &fakeExploreRankInputs{profileErr: errors.New("profile unavailable")},
		store: store, logger: log.New(&bytes.Buffer{}, "", 0),
	}
	result := runner.GenerateAll(context.Background(), time.Date(2026, 9, 1, 8, 0, 0, 0, exploreTestShanghai), time.Now())
	if result.FailWriteErrors != 1 || !result.NeedsRetry() {
		t.Fatalf("result = %+v, want one retryable fail-write error", result)
	}
}

func TestSQLExploreQueueFinishRunPersistsBoundedOutcome(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var doneID, failedID int
	if err := db.QueryRow(`INSERT INTO explore_fetch_runs(window_at,status) VALUES ($1,'running') RETURNING id`, time.Now()).Scan(&doneID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_fetch_runs(window_at,status) VALUES ($1,'running') RETURNING id`, time.Now().Add(time.Minute)).Scan(&failedID); err != nil {
		t.Fatal(err)
	}
	queue := newSQLExploreQueue(db)
	if err := queue.FinishRun(doneID, nil); err != nil {
		t.Fatal(err)
	}
	if err := queue.FinishRun(failedID, errors.New(strings.Repeat("界", 1200))); err != nil {
		t.Fatal(err)
	}
	var doneStatus, failedStatus string
	var message *string
	if err := db.QueryRow(`SELECT status FROM explore_fetch_runs WHERE id=$1`, doneID).Scan(&doneStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status,error_message FROM explore_fetch_runs WHERE id=$1`, failedID).Scan(&failedStatus, &message); err != nil {
		t.Fatal(err)
	}
	if doneStatus != model.ExploreFetchRunDone || failedStatus != model.ExploreFetchRunFailed {
		t.Fatalf("statuses = %q/%q", doneStatus, failedStatus)
	}
	if message == nil || len(*message) > 1000 {
		t.Fatalf("failed error was not safely bounded: %v", message)
	}
}

func TestExploreURLDomain(t *testing.T) {
	if got := exploreURLDomain("https://WWW.Example.com:8443/feed.xml"); got != "www.example.com" {
		t.Fatalf("domain = %q", got)
	}
	if got := exploreURLDomain("not a url"); got != "" {
		t.Fatalf("invalid URL domain = %q", got)
	}
}

func TestNormalizeExploreFeedURLMatchesCatalogCanonicalization(t *testing.T) {
	got := normalizeExploreFeedURL("HTTPS://Example.COM/feed?utm_source=reader#section")
	if got != "https://example.com/feed" {
		t.Fatalf("normalized feed URL = %q", got)
	}
}

func TestNewProductionExploreCycleUsesValidatedConfig(t *testing.T) {
	cycle := newProductionExploreCycle(nil, &config.Config{
		Explore: config.ExploreConfig{FetchBatchLimit: 321, FetchConcurrency: 7},
		RSSHub:  config.RSSHubConfig{BaseURL: "http://rsshub:1200"},
	})
	if cycle.deps.batchLimit != 321 || cycle.deps.fetchConcurrency != 7 {
		t.Fatalf("production explore deps = limit %d concurrency %d", cycle.deps.batchLimit, cycle.deps.fetchConcurrency)
	}
	if cycle.deps.registry == nil || cycle.deps.queue == nil || cycle.deps.taskHandler == nil || cycle.deps.snapshots == nil {
		t.Fatalf("production explore dependencies are incomplete: %+v", cycle.deps)
	}
}

type fakeExploreUsers struct{ ids []int }

func (users *fakeExploreUsers) ListUserIDs(context.Context) ([]int, error) { return users.ids, nil }

type fakeExploreRankInputs struct{ profileErr error }

func (inputs *fakeExploreRankInputs) LoadProfile(context.Context, int, time.Time) (explorelogic.ProfileInput, error) {
	return explorelogic.ProfileInput{}, inputs.profileErr
}
func (inputs *fakeExploreRankInputs) LoadCandidates(context.Context, time.Time) ([]explorelogic.RankCandidate, error) {
	return nil, nil
}

type recoveringRankInputs struct {
	mu    sync.Mutex
	calls int
	now   time.Time
}

func (inputs *recoveringRankInputs) LoadProfile(context.Context, int, time.Time) (explorelogic.ProfileInput, error) {
	inputs.mu.Lock()
	defer inputs.mu.Unlock()
	inputs.calls++
	if inputs.calls == 1 {
		return explorelogic.ProfileInput{}, errors.New("profile temporarily unavailable")
	}
	return explorelogic.ProfileInput{}, nil
}

func (inputs *recoveringRankInputs) LoadCandidates(context.Context, time.Time) ([]explorelogic.RankCandidate, error) {
	return []explorelogic.RankCandidate{{
		SourceID: 9, Title: "Reliable source", Domain: "example.com", Topic: "engineering",
		ValidationStatus: model.ExploreValidationValid, HealthScore: 1,
		Articles: []explorelogic.RankArticle{{ID: 19, Title: "Recent article", PublishedAt: inputs.now.Add(-time.Hour), FetchedAt: inputs.now}},
	}}, nil
}

type recoveringSnapshotStore struct {
	mu        sync.Mutex
	status    string
	claims    int
	failures  int
	publishes int
}

func (store *recoveringSnapshotStore) Claim(userID int, slotAt, _ time.Time, _ time.Duration) (*repository.ExploreSnapshotClaim, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.claims++
	return &repository.ExploreSnapshotClaim{Batch: model.ExploreBatch{ID: 52, UserID: userID, SlotAt: slotAt, Status: store.status}}, true, nil
}

func (store *recoveringSnapshotStore) Publish(_ int, _ repository.ExploreSnapshotGenerationToken, values []repository.ExploreSnapshotSourceInput) (*model.ExploreBatch, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(values) == 0 {
		return nil, errors.New("empty snapshot")
	}
	store.publishes++
	store.status = model.ExploreBatchDone
	return &model.ExploreBatch{ID: 52, Status: model.ExploreBatchDone, SourceCount: len(values)}, nil
}

func (store *recoveringSnapshotStore) Fail(_ int, _ repository.ExploreSnapshotGenerationToken, _ error) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.failures++
	store.status = model.ExploreBatchFailed
	return nil
}

func (store *recoveringSnapshotStore) snapshot() (string, int, int, int) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.status, store.claims, store.failures, store.publishes
}

type fakeSnapshotStore struct {
	claim      *repository.ExploreSnapshotClaim
	claimOwned bool
	claimErr   error
	failErr    error
	lastDone   int
	failed     int
}

func (store *fakeSnapshotStore) Claim(int, time.Time, time.Time, time.Duration) (*repository.ExploreSnapshotClaim, bool, error) {
	owned := store.claimOwned
	// Preserve the original fake's default: a configured claim is owned.
	if store.claim != nil && !store.claimOwned && store.claim.Batch.Status == "" {
		owned = true
	}
	return store.claim, owned, store.claimErr
}
func (store *fakeSnapshotStore) Publish(int, repository.ExploreSnapshotGenerationToken, []repository.ExploreSnapshotSourceInput) (*model.ExploreBatch, error) {
	return nil, nil
}
func (store *fakeSnapshotStore) Fail(batchID int, _ repository.ExploreSnapshotGenerationToken, _ error) error {
	store.failed = batchID
	return store.failErr
}

func makeExploreTasks(count int) []repository.ExploreQueueTask {
	tasks := make([]repository.ExploreQueueTask, count)
	for index := range tasks {
		tasks[index] = repository.ExploreQueueTask{ID: index + 1, SourceID: index + 1, TaskType: model.ExploreFetchTaskValidateSource}
	}
	return tasks
}

func waitExplore(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for explore worker")
}
