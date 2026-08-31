package repository_test

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestExploreQueueClaimRunHardGlobalCapAndPersistedReturn(t *testing.T) {
	for _, total := range []int{0, 1, 499, 500, 501, 1200} {
		t.Run(fmt.Sprintf("pending_%d", total), func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			repo := repository.NewExploreQueueRepository(db)
			enqueueExploreTasks(t, db, repo, total, repository.ExplorePriorityRefresh)
			run, leased, err := repo.ClaimRun(time.Now(), "worker-a", time.Hour, 1200)
			if err != nil {
				t.Fatal(err)
			}
			want := min(total, 500)
			if len(leased) != want || run.ClaimedCount != want {
				t.Fatalf("claimed=%d run=%+v want=%d", len(leased), run, want)
			}
			if total == 0 && run.Status != model.ExploreFetchRunDone {
				t.Fatalf("empty run status=%q", run.Status)
			}
			for _, task := range leased {
				assertTaskMatchesDB(t, db, task)
			}
			assertRunMatchesDB(t, db, run)
			var pending, claimed int
			if err := db.QueryRow(`SELECT count(*) FILTER (WHERE status='pending'), count(*) FILTER (WHERE status='leased' AND run_id=$1) FROM explore_fetch_queue`, run.ID).Scan(&pending, &claimed); err != nil {
				t.Fatal(err)
			}
			if pending != total-want || claimed != want {
				t.Fatalf("pending=%d leased=%d want pending=%d leased=%d", pending, claimed, total-want, want)
			}
		})
	}
}

func TestExploreQueueClaimRunRejectsNonPositiveLeaseDuration(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	for _, duration := range []time.Duration{0, -time.Second} {
		if run, tasks, err := repo.ClaimRun(time.Now(), "worker", duration, 1); err == nil || run != nil || tasks != nil {
			t.Fatalf("duration %s got run=%+v tasks=%+v err=%v", duration, run, tasks, err)
		}
		if tasks, err := repo.RecoverExpired(1, "worker", duration); err == nil || tasks != nil {
			t.Fatalf("recover duration %s got tasks=%+v err=%v", duration, tasks, err)
		}
	}
	var runs int
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_runs`).Scan(&runs); err != nil || runs != 0 {
		t.Fatalf("invalid duration created runs=%d err=%v", runs, err)
	}
}

func TestExploreQueueExistingAndZeroRunsNeverAppend(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	window := time.Now().Truncate(time.Minute)
	zero, tasks, err := repo.ClaimRun(window, "one", time.Hour, 500)
	if err != nil || len(tasks) != 0 || zero.Status != model.ExploreFetchRunDone {
		t.Fatalf("empty claim run=%+v tasks=%d err=%v", zero, len(tasks), err)
	}
	sourceID := insertExploreSource(t, db, 1)
	if _, err := repo.Enqueue(sourceID, repository.ExploreTaskValidateSource, repository.ExplorePriorityRefresh); err != nil {
		t.Fatal(err)
	}
	again, tasks, err := repo.ClaimRun(window, "two", time.Hour, 500)
	if err != nil || again.ID != zero.ID || len(tasks) != 0 {
		t.Fatalf("sealed run reopened run=%+v tasks=%d err=%v", again, len(tasks), err)
	}
	_, tasks, err = repo.ClaimRun(window.Add(time.Minute), "two", time.Hour, 500)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("later run tasks=%d err=%v", len(tasks), err)
	}
}

func TestExploreQueueConcurrentDispatchersShareOneGlobalWindow(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	enqueueExploreTasks(t, db, repo, 1200, repository.ExplorePriorityRefresh)
	window := time.Now().Truncate(time.Minute)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	errs := make(chan error, 2)
	for _, owner := range []string{"one", "two"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			_, tasks, err := repo.ClaimRun(window, owner, time.Hour, 500)
			if errors.Is(err, repository.ErrExploreDispatcherBusy) {
				return
			}
			errs <- err
			results <- len(tasks)
		}(owner)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for n := range results {
		total += n
	}
	if total > 500 {
		t.Fatalf("concurrent total=%d", total)
	}
}

func TestExploreQueueEnqueueIsIdempotentAndClampsPriority(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	sourceID := insertExploreSource(t, db, 1)
	first, err := repo.Enqueue(sourceID, repository.ExploreTaskRefreshArticles, math.MinInt)
	if err != nil {
		t.Fatal(err)
	}
	if first.Priority != 0 {
		t.Fatalf("minimum priority=%d", first.Priority)
	}
	second, err := repo.Enqueue(sourceID, repository.ExploreTaskRefreshArticles, math.MaxInt)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Priority != 10000 || !second.CreatedAt.Equal(first.CreatedAt) || !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("enqueue conflict first=%+v second=%+v", first, second)
	}
}

func TestExploreQueueLeaseFencingAndRecovery(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	enqueueExploreTasks(t, db, repo, 4, repository.ExplorePriorityRefresh)
	run, leased, err := repo.ClaimRun(time.Now(), "old", time.Millisecond, 4)
	if err != nil || len(leased) != 4 {
		t.Fatalf("claim=%d err=%v", len(leased), err)
	}
	if visible, err := repo.ListLeased(run.ID, "old"); err != nil || len(visible) != 4 {
		t.Fatalf("initial visible=%d err=%v", len(visible), err)
	}
	time.Sleep(20 * time.Millisecond)
	if visible, err := repo.ListLeased(run.ID, "old"); err != nil || len(visible) != 0 {
		t.Fatalf("expired visible=%d err=%v", len(visible), err)
	}
	if err := repo.Complete(leased[0].ID, "old"); err == nil {
		t.Fatal("expired old owner completed task")
	}
	recovered, err := repo.RecoverExpired(run.ID, "new", time.Hour)
	if err != nil || len(recovered) != 4 {
		t.Fatalf("recover=%d err=%v", len(recovered), err)
	}
	if err := repo.Complete(recovered[0].ID, "old"); err == nil {
		t.Fatal("old owner completed recovered task")
	}
	if err := repo.Retry(recovered[1].ID, "old", errors.New("stale")); err == nil {
		t.Fatal("old owner retried recovered task")
	}
	if err := repo.Invalidate(recovered[2].ID, "old", errors.New("stale")); err == nil {
		t.Fatal("old owner invalidated recovered task")
	}
	var claimed int
	if err := db.QueryRow(`SELECT claimed_count FROM explore_fetch_runs WHERE id=$1`, run.ID).Scan(&claimed); err != nil || claimed != 4 {
		t.Fatalf("recovery changed run quota claimed=%d err=%v", claimed, err)
	}
	if err := repo.Complete(recovered[0].ID, "new"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Retry(recovered[1].ID, "new", errors.New("temporary")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Invalidate(recovered[2].ID, "new", errors.New(strings.Repeat("中🙂", 400))); err != nil {
		t.Fatal(err)
	}
	if err := repo.Complete(recovered[3].ID, "new"); err != nil {
		t.Fatal(err)
	}
	var persistedError string
	if err := db.QueryRow(`SELECT last_error FROM explore_fetch_queue WHERE id=$1`, recovered[2].ID).Scan(&persistedError); err != nil || len(persistedError) > 1000 || !utf8.ValidString(persistedError) {
		t.Fatalf("error len=%d valid=%t err=%v", len(persistedError), utf8.ValidString(persistedError), err)
	}
}

func TestExploreQueueRetryUsesDatabaseClockAndBackoff(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	sourceID := insertExploreSource(t, db, 1)
	if _, err := repo.Enqueue(sourceID, repository.ExploreTaskValidateSource, repository.ExplorePriorityRefresh); err != nil {
		t.Fatal(err)
	}
	window := time.Now().Truncate(time.Minute)
	for _, want := range []int{60, 120, 240} {
		_, tasks, err := repo.ClaimRun(window, "worker", time.Hour, 1)
		if err != nil || len(tasks) != 1 {
			t.Fatalf("claim tasks=%d err=%v", len(tasks), err)
		}
		if err := repo.Retry(tasks[0].ID, "worker", errors.New("temporary")); err != nil {
			t.Fatal(err)
		}
		var seconds int
		if err := db.QueryRow(`SELECT round(EXTRACT(EPOCH FROM (not_before-updated_at)))::int FROM explore_fetch_queue WHERE id=$1`, tasks[0].ID).Scan(&seconds); err != nil || seconds != want {
			t.Fatalf("retry seconds=%d want=%d err=%v", seconds, want, err)
		}
		if want != 240 {
			if _, err := db.Exec(`UPDATE explore_fetch_queue SET not_before=CURRENT_TIMESTAMP WHERE id=$1`, tasks[0].ID); err != nil {
				t.Fatal(err)
			}
			window = window.Add(time.Minute)
		}
	}
	if _, err := db.Exec(`UPDATE explore_fetch_queue SET attempts=20, not_before=CURRENT_TIMESTAMP WHERE source_id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	_, tasks, err := repo.ClaimRun(window.Add(time.Minute), "worker", time.Hour, 1)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("cap claim tasks=%d err=%v", len(tasks), err)
	}
	if err := repo.Retry(tasks[0].ID, "worker", errors.New("capped")); err != nil {
		t.Fatal(err)
	}
	var seconds int
	if err := db.QueryRow(`SELECT round(EXTRACT(EPOCH FROM (not_before-updated_at)))::int FROM explore_fetch_queue WHERE id=$1`, tasks[0].ID).Scan(&seconds); err != nil || seconds != 3600 {
		t.Fatalf("cap seconds=%d err=%v", seconds, err)
	}
}

func TestExploreQueuePriorityAndAgeOrder(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	ids := map[int]int{}
	for i, priority := range []int{repository.ExplorePriorityDirectProfile, repository.ExplorePriorityStructuredProvider, repository.ExplorePriorityRefresh, repository.ExplorePriorityRelated} {
		id := insertExploreSource(t, db, i)
		if _, err := repo.Enqueue(id, repository.ExploreTaskValidateSource, priority); err != nil {
			t.Fatal(err)
		}
		ids[priority] = id
	}
	if _, err := db.Exec(`UPDATE explore_fetch_queue SET created_at=CURRENT_TIMESTAMP-INTERVAL '1000 hours' WHERE source_id=$1`, ids[repository.ExplorePriorityRelated]); err != nil {
		t.Fatal(err)
	}
	_, tasks, err := repo.ClaimRun(time.Now(), "order", time.Hour, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 4 || tasks[0].SourceID != ids[repository.ExplorePriorityRelated] || tasks[1].Priority != repository.ExplorePriorityDirectProfile || tasks[2].Priority != repository.ExplorePriorityStructuredProvider || tasks[3].Priority != repository.ExplorePriorityRefresh {
		t.Fatalf("order=%+v", tasks)
	}
}

func enqueueExploreTasks(t *testing.T, db *sql.DB, repo *repository.ExploreQueueRepository, total, priority int) {
	t.Helper()
	for i := 0; i < total; i++ {
		if _, err := repo.Enqueue(insertExploreSource(t, db, i), repository.ExploreTaskValidateSource, priority); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
}
func insertExploreSource(t *testing.T, db *sql.DB, n int) int {
	t.Helper()
	var id int
	u := fmt.Sprintf("https://queue-%d-%d.example/feed", time.Now().UnixNano(), n)
	if err := db.QueryRow(`INSERT INTO recommended_feeds (url,title,category,language,normalized_url) VALUES ($1,$1,'test','en',$1) RETURNING id`, u).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func assertTaskMatchesDB(t *testing.T, db *sql.DB, task repository.ExploreQueueTask) {
	t.Helper()
	var got repository.ExploreQueueTask
	if err := db.QueryRow(`SELECT id,source_id,task_type,status,priority,not_before,attempts,run_id,lease_owner,lease_expires_at,last_error,created_at,updated_at,completed_at FROM explore_fetch_queue WHERE id=$1`, task.ID).Scan(&got.ID, &got.SourceID, &got.TaskType, &got.Status, &got.Priority, &got.NotBefore, &got.Attempts, &got.RunID, &got.LeaseOwner, &got.LeaseExpiresAt, &got.LastError, &got.CreatedAt, &got.UpdatedAt, &got.CompletedAt); err != nil {
		t.Fatal(err)
	}
	if got.ID != task.ID || got.Status != task.Status || !got.UpdatedAt.Equal(task.UpdatedAt) || got.LeaseExpiresAt == nil || task.LeaseExpiresAt == nil || !got.LeaseExpiresAt.Equal(*task.LeaseExpiresAt) {
		t.Fatalf("returned task differs db returned=%+v db=%+v", task, got)
	}
}
func assertRunMatchesDB(t *testing.T, db *sql.DB, run *repository.ExploreFetchRun) {
	t.Helper()
	var got repository.ExploreFetchRun
	if err := db.QueryRow(`SELECT id,window_at,status,claimed_count,started_at,completed_at,worker_id,error_message,created_at FROM explore_fetch_runs WHERE id=$1`, run.ID).Scan(&got.ID, &got.WindowAt, &got.Status, &got.ClaimedCount, &got.StartedAt, &got.CompletedAt, &got.WorkerID, &got.ErrorMessage, &got.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if got.Status != run.Status || got.ClaimedCount != run.ClaimedCount || (got.CompletedAt == nil) != (run.CompletedAt == nil) {
		t.Fatalf("returned run differs returned=%+v db=%+v", run, got)
	}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
