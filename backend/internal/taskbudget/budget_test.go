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

func TestMonitoringDenialReasonsAndKnownReset(t *testing.T) {
	for _, tc := range []struct {
		name  string
		p     taskbudget.Policy
		owner int
		want  string
	}{
		{"user daily", taskbudget.Policy{Daily: 1, GlobalDaily: 10, Concurrent: 10, GlobalConcurrent: 10, Lease: time.Minute}, 7, "user_daily"},
		{"global daily", taskbudget.Policy{Daily: 10, GlobalDaily: 1, Concurrent: 10, GlobalConcurrent: 10, Lease: time.Minute}, 7, "global_daily"},
		{"user concurrent", taskbudget.Policy{Daily: 10, GlobalDaily: 10, Concurrent: 1, GlobalConcurrent: 10, Lease: time.Minute}, 7, "user_concurrency"},
		{"global concurrent", taskbudget.Policy{Daily: 10, GlobalDaily: 10, Concurrent: 10, GlobalConcurrent: 1, Lease: time.Minute}, 7, "global_concurrency"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, done := testdb.New(t)
			defer done()
			s := taskbudget.New(db)
			var reason string
			var retry *time.Time
			s.SetObserver(func(owner int, bucket, r string, at *time.Time) {
				reason = r
				retry = at
				if owner != 7 || bucket != "ai" {
					t.Errorf("wrong dimensions")
				}
			})
			release, e := s.Acquire(context.Background(), tc.owner, "ai", 1, tc.p)
			if e != nil {
				t.Fatal(e)
			}
			defer release()
			_, e = s.Acquire(context.Background(), tc.owner, "ai", 1, tc.p)
			if !errors.Is(e, taskbudget.ErrExceeded) || reason != tc.want {
				t.Fatalf("reason %q err %v", reason, e)
			}
			if tc.want == "user_daily" || tc.want == "global_daily" {
				if retry == nil || retry.UTC().Hour() != 0 {
					t.Fatalf("reset %v", retry)
				}
			} else if retry != nil {
				t.Fatal("concurrency recovery is unknown")
			}
		})
	}
}

func TestAdministratorBackgroundDailyQuota(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,is_admin) VALUES(201,'quota-admin','unused',true),(202,'quota-user','unused',false)`); err != nil {
		t.Fatal(err)
	}
	policies, err := taskbudget.LoadPolicies()
	if err != nil {
		t.Fatal(err)
	}
	store := taskbudget.New(db)
	p := policies["background_fetch"]
	for _, tc := range []struct {
		owner, cost int
		allowed     bool
	}{
		{201, 1999, true}, {201, 1, true}, {201, 1, false},
		{202, 500, true}, {202, 1, false},
		{0, 2500, true}, {201, 1, false},
	} {
		release, err := store.Acquire(context.Background(), tc.owner, "background_fetch", tc.cost, p)
		if tc.allowed {
			if err != nil {
				t.Fatalf("owner=%d cost=%d: %v", tc.owner, tc.cost, err)
			}
			release()
		} else if !errors.Is(err, taskbudget.ErrExceeded) {
			t.Fatalf("unexpected admission: owner=%d err=%v", tc.owner, err)
		}
	}
	// A former administrator must immediately return to the ordinary daily limit.
	if _, err := db.Exec(`DELETE FROM task_budget_daily; UPDATE users SET is_admin=false WHERE id=201`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(context.Background(), 201, "background_fetch", 501, p); !errors.Is(err, taskbudget.ErrExceeded) {
		t.Fatalf("revoked administrator: %v", err)
	}
}

func TestAdministratorBackgroundQuotaConfiguration(t *testing.T) {
	t.Setenv("TASK_BACKGROUND_FETCH_ADMIN_DAILY", "2500")
	policies, err := taskbudget.LoadPolicies()
	if err != nil || policies["background_fetch"].AdminDaily != 2500 {
		t.Fatalf("policies=%+v err=%v", policies, err)
	}
	for _, value := range []string{"0", "-1", "invalid", "1000001"} {
		t.Setenv("TASK_BACKGROUND_FETCH_ADMIN_DAILY", value)
		if _, err := taskbudget.LoadPolicies(); err == nil {
			t.Fatalf("accepted invalid admin quota %q", value)
		}
	}
}
