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
		if run, tasks, err := repo.RecoverExpired("worker", duration); err == nil || run != nil || tasks != nil {
			t.Fatalf("recover duration %s got run=%+v tasks=%+v err=%v", duration, run, tasks, err)
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
	run, leased, err := repo.ClaimRun(time.Now(), "old", time.Minute, 4)
	if err != nil || len(leased) != 4 {
		t.Fatalf("claim=%d err=%v", len(leased), err)
	}
	if visible, err := repo.ListLeased(run.ID, "old"); err != nil || len(visible) != 4 {
		t.Fatalf("initial visible=%d err=%v", len(visible), err)
	}
	result, err := db.Exec(`
		UPDATE explore_fetch_queue SET lease_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 second'
		WHERE run_id = $1 AND status = 'leased' AND lease_owner = 'old'
	`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 4 {
		t.Fatalf("force expiry changed=%d err=%v", changed, err)
	}
	if visible, err := repo.ListLeased(run.ID, "old"); err != nil || len(visible) != 0 {
		t.Fatalf("expired visible=%d err=%v", len(visible), err)
	}
	if err := repo.Complete(leased[0].ID, run.ID, "old"); err == nil {
		t.Fatal("expired old owner completed task")
	}
	recoveredRun, recovered, err := repo.RecoverExpired("new", time.Hour)
	if err != nil || recoveredRun == nil || recoveredRun.ID != run.ID || len(recovered) != 4 {
		t.Fatalf("recover run=%+v tasks=%d err=%v", recoveredRun, len(recovered), err)
	}
	if err := repo.Complete(recovered[0].ID, run.ID, "old"); err == nil {
		t.Fatal("old owner completed recovered task")
	}
	if err := repo.Retry(recovered[1].ID, run.ID, "old", errors.New("stale")); err == nil {
		t.Fatal("old owner retried recovered task")
	}
	if err := repo.Invalidate(recovered[2].ID, run.ID, "old", errors.New("stale")); err == nil {
		t.Fatal("old owner invalidated recovered task")
	}
	var claimed int
	if err := db.QueryRow(`SELECT claimed_count FROM explore_fetch_runs WHERE id=$1`, run.ID).Scan(&claimed); err != nil || claimed != 4 {
		t.Fatalf("recovery changed run quota claimed=%d err=%v", claimed, err)
	}
	if err := repo.Complete(recovered[0].ID, run.ID, "new"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Retry(recovered[1].ID, run.ID, "new", errors.New("temporary")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Invalidate(recovered[2].ID, run.ID, "new", errors.New(strings.Repeat("中🙂", 400))); err != nil {
		t.Fatal(err)
	}
	if err := repo.Complete(recovered[3].ID, run.ID, "new"); err != nil {
		t.Fatal(err)
	}
	var persistedError string
	if err := db.QueryRow(`SELECT last_error FROM explore_fetch_queue WHERE id=$1`, recovered[2].ID).Scan(&persistedError); err != nil || len(persistedError) > 1000 || !utf8.ValidString(persistedError) {
		t.Fatalf("error len=%d valid=%t err=%v", len(persistedError), utf8.ValidString(persistedError), err)
	}
}

func TestExploreQueueRecoversOldestExpiredTasksWithoutCreatingOrChargingNewRun(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	enqueueExploreTasks(t, db, repo, 3, repository.ExplorePriorityRefresh)
	oldRun, oldTasks, err := repo.ClaimRun(time.Now(), "same-worker", time.Minute, 3)
	if err != nil || len(oldTasks) != 3 {
		t.Fatalf("old claim tasks=%d err=%v", len(oldTasks), err)
	}
	if _, err := db.Exec(`UPDATE explore_fetch_queue SET lease_expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE run_id=$1`, oldRun.ID); err != nil {
		t.Fatal(err)
	}
	recoveredRun, recovered, err := repo.RecoverExpired("new-worker", time.Hour)
	if err != nil || recoveredRun == nil || recoveredRun.ID != oldRun.ID || len(recovered) != 3 {
		t.Fatalf("recovered run=%+v tasks=%d err=%v", recoveredRun, len(recovered), err)
	}
	for _, task := range recovered {
		if task.RunID == nil || *task.RunID != oldRun.ID {
			t.Fatalf("recovered task run=%v, want original %d", task.RunID, oldRun.ID)
		}
	}
	if err := repo.Complete(recovered[0].ID, oldRun.ID, "same-worker"); !errors.Is(err, repository.ErrExploreLeaseNotHeld) {
		t.Fatalf("old owner completed reassigned task: %v", err)
	}
	if err := repo.Complete(recovered[0].ID, oldRun.ID, "new-worker"); err != nil {
		t.Fatalf("new owner could not complete original-run task: %v", err)
	}
	var oldClaimed, runCount int
	if err := db.QueryRow(`SELECT claimed_count FROM explore_fetch_runs WHERE id=$1`, oldRun.ID).Scan(&oldClaimed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_runs`).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if oldClaimed != 3 || oldClaimed > 500 || recoveredRun.ClaimedCount != oldClaimed || runCount != 1 {
		t.Fatalf("claimed old=%d recovered=%d run_count=%d", oldClaimed, recoveredRun.ClaimedCount, runCount)
	}

	newRun, fresh, err := repo.ClaimRun(time.Now().Add(time.Minute), "new-worker", time.Hour, 2)
	if err != nil || len(fresh) != 0 || newRun.ClaimedCount != 0 {
		t.Fatalf("fresh run captured original-run leases run=%+v tasks=%d err=%v", newRun, len(fresh), err)
	}
}

func TestExploreQueueConcurrentRecoveryHasOneOwnerAndPreservesOriginalQuota(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	db.SetMaxOpenConns(8)
	repo := repository.NewExploreQueueRepository(db)
	enqueueExploreTasks(t, db, repo, 5, repository.ExplorePriorityRefresh)
	original, tasks, err := repo.ClaimRun(time.Now(), "crashed", time.Minute, 5)
	if err != nil || len(tasks) != 5 {
		t.Fatalf("claim tasks=%d err=%v", len(tasks), err)
	}
	if _, err := db.Exec(`UPDATE explore_fetch_queue SET lease_expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE run_id=$1`, original.ID); err != nil {
		t.Fatal(err)
	}

	type recovery struct {
		run   *repository.ExploreFetchRun
		tasks []repository.ExploreQueueTask
		err   error
	}
	start := make(chan struct{})
	results := make(chan recovery, 2)
	for _, owner := range []string{"recovery-a", "recovery-b"} {
		go func(owner string) {
			<-start
			run, tasks, err := repo.RecoverExpired(owner, time.Hour)
			results <- recovery{run: run, tasks: tasks, err: err}
		}(owner)
	}
	close(start)
	winners := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			if !errors.Is(result.err, repository.ErrExploreDispatcherBusy) {
				t.Fatalf("unexpected concurrent recovery error: %v", result.err)
			}
			continue
		}
		if result.run == nil {
			continue
		}
		winners++
		if result.run.ID != original.ID || result.run.ClaimedCount != 5 || len(result.tasks) != 5 {
			t.Fatalf("winner run=%+v tasks=%d", result.run, len(result.tasks))
		}
		for _, task := range result.tasks {
			if task.RunID == nil || *task.RunID != original.ID {
				t.Fatalf("task moved runs: %+v", task)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("recovery winners=%d want=1", winners)
	}
	var claimed, runs int
	if err := db.QueryRow(`SELECT claimed_count FROM explore_fetch_runs WHERE id=$1`, original.ID).Scan(&claimed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if claimed != 5 || runs != 1 {
		t.Fatalf("claimed=%d runs=%d", claimed, runs)
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
		if err := repo.Retry(tasks[0].ID, *tasks[0].RunID, "worker", errors.New("temporary")); err != nil {
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
	if err := repo.Retry(tasks[0].ID, *tasks[0].RunID, "worker", errors.New("capped")); err != nil {
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
	if got.ID != task.ID || got.SourceID != task.SourceID || got.TaskType != task.TaskType ||
		got.Status != task.Status || got.Priority != task.Priority || !got.NotBefore.Equal(task.NotBefore) ||
		got.Attempts != task.Attempts || !equalIntPtr(got.RunID, task.RunID) ||
		!equalStringPtr(got.LeaseOwner, task.LeaseOwner) || !equalTimePtr(got.LeaseExpiresAt, task.LeaseExpiresAt) ||
		!equalStringPtr(got.LastError, task.LastError) || !got.CreatedAt.Equal(task.CreatedAt) ||
		!got.UpdatedAt.Equal(task.UpdatedAt) || !equalTimePtr(got.CompletedAt, task.CompletedAt) {
		t.Fatalf("returned task differs db returned=%+v db=%+v", task, got)
	}
}
func assertRunMatchesDB(t *testing.T, db *sql.DB, run *repository.ExploreFetchRun) {
	t.Helper()
	var got repository.ExploreFetchRun
	if err := db.QueryRow(`SELECT id,window_at,status,claimed_count,started_at,completed_at,worker_id,error_message,created_at FROM explore_fetch_runs WHERE id=$1`, run.ID).Scan(&got.ID, &got.WindowAt, &got.Status, &got.ClaimedCount, &got.StartedAt, &got.CompletedAt, &got.WorkerID, &got.ErrorMessage, &got.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if got.ID != run.ID || !got.WindowAt.Equal(run.WindowAt) || got.Status != run.Status ||
		got.ClaimedCount != run.ClaimedCount || !equalTimePtr(got.StartedAt, run.StartedAt) ||
		!equalTimePtr(got.CompletedAt, run.CompletedAt) || !equalStringPtr(got.WorkerID, run.WorkerID) ||
		!equalStringPtr(got.ErrorMessage, run.ErrorMessage) || !got.CreatedAt.Equal(run.CreatedAt) {
		t.Fatalf("returned run differs returned=%+v db=%+v", run, got)
	}
}

func equalIntPtr(a, b *int) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func equalStringPtr(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func equalTimePtr(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
