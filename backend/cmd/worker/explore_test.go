package main

import (
	"bytes"
	"context"
	"errors"
	"log"
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
	mu          sync.Mutex
	claimCalls  int
	windows     []time.Time
	limits      []int
	owners      []string
	tasks       []repository.ExploreQueueTask
	finishedRun []int
}

func (queue *fakeExploreQueue) ClaimRun(window time.Time, owner string, _ time.Duration, limit int) (*repository.ExploreFetchRun, []repository.ExploreQueueTask, error) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.claimCalls++
	queue.windows = append(queue.windows, window)
	queue.limits = append(queue.limits, limit)
	queue.owners = append(queue.owners, owner)
	runID := 71
	tasks := append([]repository.ExploreQueueTask(nil), queue.tasks...)
	for index := range tasks {
		tasks[index].RunID = &runID
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

func (handler *fakeExploreTaskHandler) Process(_ context.Context, task repository.ExploreQueueTask, _ string) error {
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

func (runner *fakeExploreSnapshotRunner) GenerateAll(_ context.Context, slotAt, _ time.Time) bool {
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
	return retry
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

type fakeSnapshotStore struct {
	claim    *repository.ExploreSnapshotClaim
	lastDone int
	failed   int
}

func (store *fakeSnapshotStore) Claim(int, time.Time, time.Time, time.Duration) (*repository.ExploreSnapshotClaim, bool, error) {
	return store.claim, true, nil
}
func (store *fakeSnapshotStore) Publish(int, repository.ExploreSnapshotGenerationToken, []repository.ExploreSnapshotSourceInput) (*model.ExploreBatch, error) {
	return nil, nil
}
func (store *fakeSnapshotStore) Fail(batchID int, _ repository.ExploreSnapshotGenerationToken, _ error) error {
	store.failed = batchID
	return nil
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
