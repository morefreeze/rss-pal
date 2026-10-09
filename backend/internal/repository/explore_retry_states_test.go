package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"os"
	"testing"
	"time"
)

func TestSourceRetryBudgetSurvivesNewTasksAndStops(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	source := insertProcessorSource(t, db, "https://retry-budget.example/feed", model.ExploreValidationPending)
	repo := NewExploreQueueRepository(db)
	intervals := []float64{3600, 14400, 57600, 172800, 691200, 2764800}
	for i := 0; i < 7; i++ {
		task := leaseProcessorTask(t, db, source, ExploreTaskValidateSource, 300, "retry-worker")
		if err := NewExploreTaskProcessor(db, &fakeExploreSourceFetcher{err: errors.New("network timeout")}, time.Now).Process(context.Background(), task); err != nil {
			t.Fatal(err)
		}
		var state string
		var failures int
		var delay *float64
		if err := db.QueryRow(`SELECT fetch_state,fetch_failures,extract(epoch from(next_retry_at-CURRENT_TIMESTAMP))::float8 FROM recommended_feeds WHERE id=$1`, source).Scan(&state, &failures, &delay); err != nil {
			t.Fatal(err)
		}
		if failures != i+1 {
			t.Fatalf("failure %d count=%d", i, failures)
		}
		if i == 6 {
			if state != "retry_exhausted" || delay != nil {
				t.Fatalf("exhausted state=%s delay=%v", state, delay)
			}
			break
		}
		if state != "retry_wait" || delay == nil || *delay < intervals[i]*.89 || *delay > intervals[i]*1.11 {
			t.Fatalf("retry %d state=%s delay=%v", i, state, delay)
		}
		// A producer cannot bypass the source deadline, even with another task type.
		if got, err := repo.Enqueue(source, ExploreTaskRefreshArticles, 800); err != nil || got != nil {
			t.Fatalf("early enqueue: %+v %v", got, err)
		}
		// Replace a pending task to prove the source budget survives its lifecycle.
		if _, err := db.Exec(`UPDATE recommended_feeds SET next_retry_at=now()-interval '1 second' WHERE id=$1`, source); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE explore_fetch_queue SET status='invalid' WHERE source_id=$1 AND status='pending'`, source); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := repo.Enqueue(source, ExploreTaskValidateSource, 300); err != nil || got != nil {
		t.Fatalf("stopped enqueue %+v %v", got, err)
	}
}

func TestSourceTerminalStopsPendingSiblingAndKeepsRecord(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	source := insertProcessorSource(t, db, "https://terminal-retry.example/feed", model.ExploreValidationPending)
	task := leaseProcessorTask(t, db, source, ExploreTaskValidateSource, 300, "terminal-worker")
	repo := NewExploreQueueRepository(db)
	if _, err := repo.Enqueue(source, ExploreTaskRefreshArticles, 200); err != nil {
		t.Fatal(err)
	}
	if err := NewExploreTaskProcessor(db, &fakeExploreSourceFetcher{err: explore.ErrInsufficientSourceConfidence}, time.Now).Process(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	var state string
	var live int
	if err := db.QueryRow(`SELECT fetch_state FROM recommended_feeds WHERE id=$1`, source).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_queue WHERE source_id=$1 AND status IN ('pending','leased')`, source).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if state != "ineligible" || live != 0 {
		t.Fatalf("state=%s live=%d", state, live)
	}
	if got, err := repo.Enqueue(source, ExploreTaskValidateSource, 300); err != nil || got != nil {
		t.Fatalf("terminal enqueue %+v %v", got, err)
	}
}

func TestRetrySuccessResetsSourceBudget(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	source := insertProcessorSource(t, db, "https://retry-success.example/feed", model.ExploreValidationValid)
	if _, err := db.Exec(`UPDATE recommended_feeds SET fetch_state='retry_wait',fetch_failures=6,next_retry_at=now()-interval '1 second' WHERE id=$1`, source); err != nil {
		t.Fatal(err)
	}
	task := leaseProcessorTask(t, db, source, ExploreTaskRefreshArticles, 200, "success-worker")
	now := time.Now().UTC()
	if err := NewExploreTaskProcessor(db, &fakeExploreSourceFetcher{result: processorSuccessResult(now, "https://retry-success.example/feed")}, func() time.Time { return now }).Process(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	var state string
	var failures int
	var cleared bool
	if err := db.QueryRow(`SELECT fetch_state,fetch_failures,next_retry_at IS NULL FROM recommended_feeds WHERE id=$1`, source).Scan(&state, &failures, &cleared); err != nil {
		t.Fatal(err)
	}
	if state != "active" || failures != 0 || !cleared {
		t.Fatalf("%s %d %v", state, failures, cleared)
	}
}

func TestMigration053PreservesHistoryAndClassifiesSources(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "052_reddit_browser_transport.sql")
	defer cleanup()
	cases := []struct {
		reason, want string
		attempts     int
	}{
		{"HTML page has no supported feed alternate", "ineligible", 0},
		{"source has insufficient public observation evidence", "ineligible", 1},
		{"parse source: feed has no article published or updated in the last 90 days", "ineligible", 2},
		{"fetch source: unexpected HTTP status 403", "retry_wait", 0},
		{"fetch source: unexpected HTTP status 410", "unavailable", 0},
		{"network timeout", "retry_exhausted", 95},
		{"network timeout", "retry_wait", 6},
		{"network timeout", "retry_exhausted", 7},
	}
	ids := []int{}
	for i, c := range cases {
		id := insertProcessorSource(t, db, fmt.Sprintf("https://migrate-retry-%d.example/feed", i), model.ExploreValidationInvalid)
		ids = append(ids, id)
		if _, err := db.Exec(`UPDATE recommended_feeds SET last_error=$2,is_broken=true WHERE id=$1`, id, c.reason); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO explore_fetch_queue(source_id,task_type,attempts,last_error) VALUES($1,'validate_source',$2,$3)`, id, c.attempts, c.reason); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../../migrations/053_explore_retry_states.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		var state, queueState, reason string
		if err := db.QueryRow(`SELECT s.fetch_state,q.status,q.last_error FROM recommended_feeds s JOIN explore_fetch_queue q ON q.source_id=s.id WHERE s.id=$1`, ids[i]).Scan(&state, &queueState, &reason); err != nil {
			t.Fatal(err)
		}
		wantQueue := "invalid"
		if c.want == "retry_wait" {
			wantQueue = "pending"
		}
		if state != c.want || queueState != wantQueue || reason != c.reason {
			t.Fatalf("%+v: %s/%s/%s", c, state, queueState, reason)
		}
	}
}

func TestLeasedSiblingHonorsSourceBackoff(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	source := insertProcessorSource(t, db, "https://sibling-retry.example/feed", model.ExploreValidationValid)
	task := leaseProcessorTask(t, db, source, ExploreTaskRefreshArticles, 200, "worker")
	if _, err := db.Exec(`UPDATE recommended_feeds SET fetch_state='retry_wait',fetch_failures=1,next_retry_at=now()+interval '1 hour' WHERE id=$1`, source); err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeExploreSourceFetcher{err: errors.New("unexpected fetch")}
	if err := NewExploreTaskProcessor(db, fetcher, time.Now).Process(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	calls, _ := fetcher.snapshot()
	if calls != 0 {
		t.Fatalf("issued %d requests during backoff", calls)
	}
	assertProcessorTaskStatus(t, db, task.ID, model.ExploreFetchTaskPending)
	var failures int
	if err := db.QueryRow(`SELECT fetch_failures FROM recommended_feeds WHERE id=$1`, source).Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if failures != 1 {
		t.Fatalf("deferral consumed retry: %d", failures)
	}
}

func TestAliasCannotReactivateStoppedCanonicalSource(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	canonical := insertProcessorSource(t, db, "https://canonical-stopped.example/feed", model.ExploreValidationInvalid)
	if _, err := db.Exec(`UPDATE recommended_feeds SET fetch_state='retry_exhausted',fetch_failures=7 WHERE id=$1`, canonical); err != nil {
		t.Fatal(err)
	}
	alias := insertProcessorSource(t, db, "https://canonical-stopped.example/", model.ExploreValidationPending)
	task := leaseProcessorTask(t, db, alias, ExploreTaskValidateSource, 300, "alias-worker")
	now := time.Now().UTC()
	if err := NewExploreTaskProcessor(db, &fakeExploreSourceFetcher{result: processorSuccessResult(now, "https://canonical-stopped.example/feed")}, func() time.Time { return now }).Process(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	var state string
	var failures int
	if err := db.QueryRow(`SELECT fetch_state,fetch_failures FROM recommended_feeds WHERE id=$1`, canonical).Scan(&state, &failures); err != nil {
		t.Fatal(err)
	}
	if state != "retry_exhausted" || failures != 7 {
		t.Fatalf("canonical revived: %s %d", state, failures)
	}
}

func TestConcurrentAliasAndCanonicalValidation(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	canonical := insertProcessorSource(t, db, "https://concurrent-retry.example/feed", model.ExploreValidationPending)
	alias := insertProcessorSource(t, db, "https://concurrent-retry.example/", model.ExploreValidationPending)
	a := leaseProcessorTask(t, db, alias, ExploreTaskValidateSource, 300, "alias")
	b := leaseProcessorTask(t, db, canonical, ExploreTaskValidateSource, 300, "canonical")
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	results := make(chan error, 2)
	now := time.Now().UTC()
	for _, task := range []ExploreQueueTask{a, b} {
		go func(task ExploreQueueTask) {
			fetcher := &fakeExploreSourceFetcher{result: processorSuccessResult(now, "https://concurrent-retry.example/feed"), hook: func() { ready <- struct{}{}; <-release }}
			results <- NewExploreTaskProcessor(db, fetcher, func() time.Time { return now }).Process(context.Background(), task)
		}(task)
	}
	<-ready
	<-ready
	close(release)
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("validation lock ordering blocked")
		}
	}
}
