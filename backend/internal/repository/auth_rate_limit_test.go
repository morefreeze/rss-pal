package repository_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestAuthRateLimitAtomicAcrossInstancesAndExpiry(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	first := repository.NewAuthRateLimitRepository(db)
	second := repository.NewAuthRateLimitRepository(db)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			repo := first
			if i%2 == 1 {
				repo = second
			}
			ok, retry, err := repo.Allow(context.Background(), "hashed-key", 5, time.Hour)
			if err != nil {
				t.Error(err)
				return
			}
			if retry <= 0 {
				t.Error("missing retry interval")
			}
			if ok {
				allowed.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if got := allowed.Load(); got != 5 {
		t.Fatalf("allowed %d; want 5", got)
	}
	// A fresh repository is a process restart; the exhausted budget must survive.
	if ok, _, err := repository.NewAuthRateLimitRepository(db).Allow(context.Background(), "hashed-key", 5, time.Hour); err != nil || ok {
		t.Fatalf("restart reset budget: %v %v", ok, err)
	}
	if _, err := db.Exec(`UPDATE auth_rate_limits SET expires_at=NOW()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if ok, _, err := second.Allow(context.Background(), "hashed-key", 5, time.Hour); err != nil || !ok {
		t.Fatalf("expired budget not reset: %v %v", ok, err)
	}
}

func TestAuthRateLimitCleanupAndUnavailable(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewAuthRateLimitRepository(db)
	for i := 0; i < 3; i++ {
		if _, _, err := repo.Allow(t.Context(), fmt.Sprint(i), 1, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE auth_rate_limits SET expires_at=NOW()-interval '1 second' WHERE bucket_key <> '2'`); err != nil {
		t.Fatal(err)
	}
	if err := repo.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM auth_rate_limits`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("cleanup: count %d error %v", count, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, _, err := repo.Allow(ctx, "cancelled", 1, time.Minute); ok || err == nil {
		t.Fatal("storage failure allowed request")
	}
}

func TestAuthRateLimitPrunePreservesConcurrentRenewal(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewAuthRateLimitRepository(db)
	if _, _, err := repo.Allow(t.Context(), "renewed", 1, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE auth_rate_limits SET expires_at=NOW()-interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE auth_rate_limits SET expires_at=NOW()+interval '1 hour' WHERE bucket_key='renewed'`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- repo.Prune(t.Context()) }()
	// Wait until DELETE has taken a snapshot of the old row and is waiting for
	// our update, OR returns immediately when using SKIP LOCKED.
	deadline := time.Now().Add(3 * time.Second)
	finished := false
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			finished = true
		default:
		}
		if finished {
			break
		}
		var blocked bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'DELETE FROM auth_rate_limits%')`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if !finished {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM auth_rate_limits WHERE bucket_key='renewed'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("cleanup deleted renewed budget")
	}
}
