package repository_test

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestExploreQueueClaimRunHardGlobalCap(t *testing.T) {
	for _, total := range []int{0, 1, 499, 500, 501, 1200} {
		t.Run(fmt.Sprintf("pending_%d", total), func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			repo := repository.NewExploreQueueRepository(db)
			enqueueExploreTasks(t, db, repo, total, repository.ExplorePriorityRefresh)

			run, leased, err := repo.ClaimRun(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC), "worker-a", time.Now().Add(time.Hour), 1200)
			if err != nil {
				t.Fatal(err)
			}
			want := total
			if want > 500 {
				want = 500
			}
			if len(leased) != want || run.ClaimedCount != want {
				t.Fatalf("claimed=%d run.claimed_count=%d, want %d", len(leased), run.ClaimedCount, want)
			}
			var pending, claimed int
			if err := db.QueryRow(`SELECT count(*) FILTER (WHERE status = 'pending'), count(*) FILTER (WHERE status = 'leased' AND run_id = $1) FROM explore_fetch_queue`, run.ID).Scan(&pending, &claimed); err != nil {
				t.Fatal(err)
			}
			if pending != total-want || claimed != want {
				t.Fatalf("pending=%d leased=%d, want pending=%d leased=%d", pending, claimed, total-want, want)
			}
		})
	}
}

func TestExploreQueueExistingRunNeverAppendsAndLaterWindowClaimsRemainder(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	enqueueExploreTasks(t, db, repo, 501, repository.ExplorePriorityRefresh)
	window := time.Date(2026, 8, 31, 11, 0, 0, 0, time.UTC)
	first, leased, err := repo.ClaimRun(window, "one", time.Now().Add(time.Hour), 500)
	if err != nil || len(leased) != 500 {
		t.Fatalf("first claim: leased=%d err=%v", len(leased), err)
	}
	again, leased, err := repo.ClaimRun(window, "two", time.Now().Add(time.Hour), 500)
	if err != nil || again.ID != first.ID || len(leased) != 0 || again.ClaimedCount != 500 {
		t.Fatalf("same run appended: run=%+v leased=%d err=%v", again, len(leased), err)
	}
	_, remainder, err := repo.ClaimRun(window.Add(time.Minute), "two", time.Now().Add(time.Hour), 500)
	if err != nil || len(remainder) != 1 {
		t.Fatalf("later run remainder=%d err=%v", len(remainder), err)
	}
}

func TestExploreQueueConcurrentDispatchersShareOneGlobalWindow(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	enqueueExploreTasks(t, db, repo, 1200, repository.ExplorePriorityRefresh)
	window := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	errs := make(chan error, 2)
	for _, owner := range []string{"one", "two"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			_, tasks, err := repo.ClaimRun(window, owner, time.Now().Add(time.Hour), 500)
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
		t.Fatalf("concurrent claims total=%d, want <=500", total)
	}
	var claimed int
	if err := db.QueryRow(`SELECT claimed_count FROM explore_fetch_runs WHERE window_at = $1`, window).Scan(&claimed); err != nil {
		t.Fatal(err)
	}
	if claimed > 500 {
		t.Fatalf("persisted claimed_count=%d, want <=500", claimed)
	}
}

func TestExploreQueueEnqueueConflictPreservesCreatedAt(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	sourceID := insertExploreSource(t, db, 1)
	first, err := repo.Enqueue(sourceID, repository.ExploreTaskRefreshArticles, repository.ExplorePriorityRelated)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	second, err := repo.Enqueue(sourceID, repository.ExploreTaskRefreshArticles, repository.ExplorePriorityDirectProfile)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.Priority != repository.ExplorePriorityDirectProfile || !second.CreatedAt.Equal(first.CreatedAt) || !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("conflict result first=%+v second=%+v", first, second)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_queue WHERE source_id = $1`, sourceID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("row count=%d err=%v", count, err)
	}
}

func TestExploreQueueRecoveryAndTerminalTransitions(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	fixedNow := time.Date(2026, 8, 31, 13, 0, 0, 0, time.UTC)
	repo := repository.NewExploreQueueRepository(db).WithClock(func() time.Time { return fixedNow })
	enqueueExploreTasks(t, db, repo, 3, repository.ExplorePriorityRefresh)
	run, leased, err := repo.ClaimRun(fixedNow, "old", fixedNow.Add(-time.Minute), 3)
	if err != nil || len(leased) != 3 {
		t.Fatalf("claim: %d %v", len(leased), err)
	}
	recovered, err := repo.RecoverExpired(run.ID, "new", fixedNow.Add(time.Hour))
	if err != nil || len(recovered) != 3 {
		t.Fatalf("recover: %d %v", len(recovered), err)
	}
	if run.ClaimedCount != 3 {
		t.Fatalf("recovery changed in-memory quota: %d", run.ClaimedCount)
	}
	var claimed, sameRun int
	if err := db.QueryRow(`SELECT r.claimed_count, count(q.id) FROM explore_fetch_runs r LEFT JOIN explore_fetch_queue q ON q.run_id = r.id AND q.status = 'leased' WHERE r.id = $1 GROUP BY r.claimed_count`, run.ID).Scan(&claimed, &sameRun); err != nil || claimed != 3 || sameRun != 3 {
		t.Fatalf("recovery persisted claim=%d sameRun=%d err=%v", claimed, sameRun, err)
	}
	if err := repo.Retry(recovered[0].ID, errors.New("temporary failure")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Invalidate(recovered[1].ID, errors.New("permanent failure")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Complete(recovered[2].ID); err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	var notBefore time.Time
	var nullRun sql.NullInt64
	if err := db.QueryRow(`SELECT status, attempts, not_before, run_id FROM explore_fetch_queue WHERE id = $1`, recovered[0].ID).Scan(&status, &attempts, &notBefore, &nullRun); err != nil || status != "pending" || attempts != 1 || nullRun.Valid || !notBefore.Equal(fixedNow.Add(time.Minute)) {
		t.Fatalf("retry state status=%s attempts=%d notBefore=%v run=%+v err=%v", status, attempts, notBefore, nullRun, err)
	}
	for _, tc := range []struct {
		id   int
		want string
	}{{recovered[1].ID, "invalid"}, {recovered[2].ID, "done"}} {
		if err := db.QueryRow(`SELECT status FROM explore_fetch_queue WHERE id = $1`, tc.id).Scan(&status); err != nil || status != tc.want {
			t.Fatalf("terminal status id=%d got=%s err=%v", tc.id, status, err)
		}
	}
}

func TestExploreQueuePriorityAndAgeOrder(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewExploreQueueRepository(db)
	ids := make(map[string]int)
	for i, priority := range []int{repository.ExplorePriorityDirectProfile, repository.ExplorePriorityStructuredProvider, repository.ExplorePriorityRefresh, repository.ExplorePriorityRelated} {
		id := insertExploreSource(t, db, i+1)
		if _, err := repo.Enqueue(id, repository.ExploreTaskValidateSource, priority); err != nil {
			t.Fatal(err)
		}
		ids[fmt.Sprint(priority)] = id
	}
	// An old related task must eventually pass fresh high-priority work.
	if _, err := db.Exec(`UPDATE explore_fetch_queue SET created_at = NOW() - INTERVAL '1000 hours' WHERE source_id = $1`, ids[fmt.Sprint(repository.ExplorePriorityRelated)]); err != nil {
		t.Fatal(err)
	}
	_, tasks, err := repo.ClaimRun(time.Date(2026, 8, 31, 14, 0, 0, 0, time.UTC), "order", time.Now().Add(time.Hour), 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 4 || tasks[0].SourceID != ids[fmt.Sprint(repository.ExplorePriorityRelated)] || tasks[1].Priority != repository.ExplorePriorityDirectProfile || tasks[2].Priority != repository.ExplorePriorityStructuredProvider || tasks[3].Priority != repository.ExplorePriorityRefresh {
		t.Fatalf("unexpected priority order: %+v", tasks)
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
	url := fmt.Sprintf("https://queue-%d-%d.example/feed", time.Now().UnixNano(), n)
	if err := db.QueryRow(`INSERT INTO recommended_feeds (url, title, category, language, normalized_url) VALUES ($1, $1, 'test', 'en', $1) RETURNING id`, url).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
