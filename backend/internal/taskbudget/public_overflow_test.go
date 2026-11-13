package taskbudget_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/taskbudget"
)

func TestPublicOverflowSharesPlatformCaps(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	p := taskbudget.Policy{Daily: 2, GlobalDaily: 4, Concurrent: 1, GlobalConcurrent: 2, Lease: time.Minute, PublicOverflow: true}
	a, b := taskbudget.New(db), taskbudget.New(db)
	var personal, public atomic.Int32
	for _, store := range []*taskbudget.Store{a, b} {
		store.SetAdmissionObserver(func(_ string, outcome string) {
			switch outcome {
			case "public":
				public.Add(1)
			case "personal":
				personal.Add(1)
			}
		})
	}
	const bucket = "background_fetch"
	first, err := a.Acquire(t.Context(), 7, bucket, 1, p)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	var wg sync.WaitGroup
	releases := make(chan func(), 8)
	for range 8 {
		wg.Go(func() {
			release, err := b.Acquire(t.Context(), 7, bucket, 1, p)
			var limit *taskbudget.LimitError
			if err == nil {
				releases <- release
			} else if !errors.As(err, &limit) || limit.Reason != "global_concurrency" {
				t.Errorf("unexpected admission result: %v", err)
			}
		})
	}
	wg.Wait()
	close(releases)
	for release := range releases {
		release()
	}
	first()
	if personal.Load() != 1 || public.Load() != 1 {
		t.Fatalf("concurrency outcomes personal=%d public=%d", personal.Load(), public.Load())
	}

	for range 2 {
		release, err := a.Acquire(t.Context(), 7, bucket, 1, p)
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	_, err = a.Acquire(t.Context(), 8, bucket, 1, p)
	var limit *taskbudget.LimitError
	if !errors.As(err, &limit) || limit.Reason != "global_daily" {
		t.Fatalf("want platform daily rejection, got %v", err)
	}
	var owner, platform int
	if err := db.QueryRow(`SELECT sum(used) FILTER(WHERE owner_id=7),sum(used) FILTER(WHERE owner_id=0)
		FROM task_budget_daily WHERE bucket=$1`, bucket).Scan(&owner, &platform); err != nil {
		t.Fatal(err)
	}
	if owner != 2 || platform != 4 || personal.Load() != 2 || public.Load() != 2 {
		t.Fatalf("owner=%d platform=%d personal=%d public=%d", owner, platform, personal.Load(), public.Load())
	}
	release, err := b.Acquire(t.Context(), 7, "subscription_fetch", 1, p)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
