package taskbudget_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/taskbudget"
)

func TestAtomicDailyBudgetAcrossInstances(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	a, b := taskbudget.New(db), taskbudget.New(db)
	policy := taskbudget.Policy{Daily: 3, GlobalDaily: 100, Concurrent: 100, GlobalConcurrent: 100, Lease: time.Minute}
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := a
			if i%2 == 0 {
				store = b
			}
			release, err := store.Acquire(context.Background(), 7, "ai", 1, policy)
			if err == nil {
				admitted.Add(1)
				release()
			} else if !errors.Is(err, taskbudget.ErrExceeded) {
				t.Errorf("acquire: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if admitted.Load() != 3 {
		t.Fatalf("admitted %d, want exactly 3", admitted.Load())
	}
	// Releasing concurrency must not refund the spent daily quota.
	if _, err := a.Acquire(context.Background(), 7, "ai", 1, policy); !errors.Is(err, taskbudget.ErrExceeded) {
		t.Fatalf("spent quota reused: %v", err)
	}
	release, err := a.Acquire(context.Background(), 8, "ai", 1, policy)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestConcurrencyExpiryAndFailClosed(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	store := taskbudget.New(db)
	p := taskbudget.Policy{Daily: 20, GlobalDaily: 100, Concurrent: 1, GlobalConcurrent: 2, Lease: time.Minute}
	release, err := store.Acquire(context.Background(), 1, "capture", 1, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(context.Background(), 1, "capture", 1, p); !errors.Is(err, taskbudget.ErrExceeded) {
		t.Fatalf("concurrency admitted: %v", err)
	}
	if _, err := db.Exec(`UPDATE task_budget_leases SET expires_at=NOW()-INTERVAL '1 second'`); err != nil {
		t.Fatal(err)
	}
	second, err := store.Acquire(context.Background(), 1, "capture", 1, p)
	if err != nil {
		t.Fatal(err)
	}
	release()
	second()
	if _, err := db.Exec(`DROP TABLE task_budget_daily`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(context.Background(), 1, "capture", 1, p); err == nil {
		t.Fatal("database failure allowed task")
	}
}

func TestGlobalDailyAcrossOwnersAndUTCDayReset(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	store := taskbudget.New(db)
	p := taskbudget.Policy{Daily: 5, GlobalDaily: 3, Concurrent: 1, GlobalConcurrent: 2, Lease: time.Minute}
	// Old date usage must not prevent today's work, for either ledger row.
	if _, err := db.Exec(`INSERT INTO task_budget_daily(owner_id,bucket,day,used) VALUES(0,'ai',(clock_timestamp() AT TIME ZONE 'UTC')::date-1,100),(1,'ai',(clock_timestamp() AT TIME ZONE 'UTC')::date-1,100)`); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []int{1, 2, 0} {
		release, err := store.Acquire(context.Background(), owner, "ai", 1, p)
		if err != nil {
			t.Fatalf("owner %d: %v", owner, err)
		}
		release()
	}
	if _, err := store.Acquire(context.Background(), 3, "ai", 1, p); !errors.Is(err, taskbudget.ErrExceeded) {
		t.Fatalf("global budget bypassed: %v", err)
	}
	var used int
	if err := db.QueryRow(`SELECT used FROM task_budget_daily WHERE owner_id=0 AND bucket='ai' AND day=(clock_timestamp() AT TIME ZONE 'UTC')::date`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != 3 {
		t.Fatalf("system work double charged: %d", used)
	}
}
