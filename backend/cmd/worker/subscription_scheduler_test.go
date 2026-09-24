package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestWorkerLoopsKeepPollingWhileBackfillBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	ticks := make(chan struct{}, 20)
	done := make(chan struct{})
	var backfills atomic.Int32
	go func() {
		defer close(done)
		runWorkerLoops(ctx, 5*time.Millisecond, func(context.Context) { ticks <- struct{}{} }, func(ctx context.Context) { backfills.Add(1); close(started); <-ctx.Done() })
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("backfill did not start")
	}
	for i := 0; i < 3; i++ {
		select {
		case <-ticks:
		case <-time.After(time.Second):
			t.Fatal("subscription polling waited for backfill")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loops did not stop")
	}
	if backfills.Load() != 1 {
		t.Fatalf("overlapping backfill: %d", backfills.Load())
	}
}

func TestWorkerLoopsDoNotOverlapSubscriptionChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int32
	go func() {
		defer close(done)
		runWorkerLoops(ctx, time.Millisecond, func(ctx context.Context) {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-ctx.Done()
		}, func(context.Context) {})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("check did not start")
	}
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatal("subscription checks overlapped")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop cancellation hung")
	}
}

func TestSubscriptionDispatcherFairBoundsAndDrain(t *testing.T) {
	ownerA, ownerB := 1, 2
	feeds := []model.Feed{{ID: 1, OwnerID: &ownerA}, {ID: 2, OwnerID: &ownerA}, {ID: 3, OwnerID: &ownerA}, {ID: 4, OwnerID: &ownerB}, {ID: 5, OwnerID: &ownerB}}
	started := make(chan model.Feed, 5)
	release := make(chan struct{})
	done := make(chan struct{})
	var mu sync.Mutex
	active := map[int]int{}
	maxTotal := 0
	completed := 0
	go func() {
		defer close(done)
		dispatchSubscriptionFeeds(context.Background(), feeds, 2, 1, func(ctx context.Context, f model.Feed) {
			mu.Lock()
			active[*f.OwnerID]++
			total := active[1] + active[2]
			if total > maxTotal {
				maxTotal = total
			}
			if active[*f.OwnerID] > 1 {
				t.Error("owner limit exceeded")
			}
			mu.Unlock()
			started <- f
			<-release
			mu.Lock()
			active[*f.OwnerID]--
			completed++
			mu.Unlock()
		})
	}()
	var first, second model.Feed
	select {
	case first = <-started:
	case <-time.After(time.Second):
		t.Fatal("no first task")
	}
	select {
	case second = <-started:
	case <-time.After(time.Second):
		t.Fatal("one owner blocked all slots")
	}
	if *first.OwnerID == *second.OwnerID {
		t.Fatal("different owners must get a slot")
	}
	select {
	case <-started:
		t.Fatal("global limit exceeded")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("queued tasks did not drain")
	}
	if completed != len(feeds) || maxTotal > 2 {
		t.Fatalf("completed=%d maxTotal=%d", completed, maxTotal)
	}
}

func TestSubscriptionDispatcherCancelDrainsRunningTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := 1
	feeds := []model.Feed{{ID: 1, OwnerID: &owner}, {ID: 2, OwnerID: &owner}}
	started := make(chan struct{})
	done := make(chan struct{})
	var calls, exited atomic.Int32
	go func() {
		defer close(done)
		dispatchSubscriptionFeeds(ctx, feeds, 1, 1, func(ctx context.Context, _ model.Feed) { calls.Add(1); close(started); <-ctx.Done(); exited.Add(1) })
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("task did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatch did not stop")
	}
	if calls.Load() != 1 || exited.Load() != 1 {
		t.Fatal("queued work started after cancel or running task leaked")
	}
	dispatchSubscriptionFeeds(ctx, feeds, 1, 1, func(context.Context, model.Feed) { t.Error("started on canceled context") })
}
