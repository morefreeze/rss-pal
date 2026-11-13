package taskbudget_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/taskbudget"
)

func TestBackgroundAcquireWaitsWithoutDenialOrExtraCharge(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	active, waiting := taskbudget.New(db), taskbudget.New(db)
	p := taskbudget.Policy{Daily: 2, GlobalDaily: 2, Concurrent: 1, GlobalConcurrent: 1, Lease: time.Minute, PublicOverflow: true}
	const bucket = "background_fetch"
	var denials atomic.Int32
	waiting.SetObserver(func(int, string, string, *time.Time) { denials.Add(1) })
	release, err := active.Acquire(t.Context(), 7, bucket, 1, p)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	checked := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		release, err := waiting.AcquireWaiting(ctx, 7, bucket, 1, p, func(context.Context) (bool, error) {
			select {
			case checked <- struct{}{}:
			default:
			}
			return true, nil
		})
		if err == nil {
			release()
		}
		done <- err
	}()
	for range 2 {
		select {
		case <-checked:
		case err := <-done:
			t.Fatalf("exited while no slot was free: %v", err)
		case <-ctx.Done():
			t.Fatal("did not retry admission")
		}
	}
	var used int
	if err := db.QueryRow(`SELECT used FROM task_budget_daily WHERE owner_id=0 AND bucket=$1`, bucket).Scan(&used); err != nil || used != 1 {
		t.Fatalf("waiting charged quota: used=%d err=%v", used, err)
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("did not resume after a slot opened")
	}
	if denials.Load() != 0 {
		t.Fatalf("waiting emitted %d denials", denials.Load())
	}
	_, err = waiting.AcquireWaiting(ctx, 7, bucket, 1, p, nil)
	var limit *taskbudget.LimitError
	if !errors.As(err, &limit) || limit.Reason != "global_daily" {
		t.Fatalf("daily quota must still be enforced: %v", err)
	}
}

func TestBackgroundAcquireStopsWhenTaskBecomesIneligible(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	store := taskbudget.New(db)
	p := taskbudget.Policy{Daily: 5, GlobalDaily: 5, Concurrent: 1, GlobalConcurrent: 2, Lease: time.Minute}
	const bucket = "background_fetch"
	release, err := store.Acquire(t.Context(), 7, bucket, 1, p)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	checks := 0
	store.SetObserver(func(_ int, _, reason string, _ *time.Time) {
		t.Errorf("waiting emitted a rejection: %s", reason)
	})
	_, err = store.AcquireWaiting(ctx, 7, bucket, 1, p, func(context.Context) (bool, error) {
		checks++
		return checks == 1, nil
	})
	if !errors.Is(err, taskbudget.ErrNoLongerEligible) || checks != 2 {
		t.Fatalf("stale task acquired or was not rechecked: checks=%d err=%v", checks, err)
	}
}
