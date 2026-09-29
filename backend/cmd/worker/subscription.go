package main

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
)

// Each loop owns its ticker and runs at most one cycle at a time. Slow content
// backfills cannot delay subscription checks, and cancellation joins both loops.
func runWorkerLoops(ctx context.Context, interval time.Duration, subscriptions, backfill func(context.Context)) {
	var loops sync.WaitGroup
	for _, cycle := range []func(context.Context){subscriptions, backfill} {
		loops.Add(1)
		go func(cycle func(context.Context)) {
			defer loops.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for ctx.Err() == nil {
				cycle(ctx)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}(cycle)
	}
	loops.Wait()
}

// Dispatch round-robin between owners, oldest-check-first within each owner.
// Only running tasks get goroutines; waiting feeds consume no budget or slots.
// The database ledger remains authoritative across worker processes.
func dispatchSubscriptionFeeds(ctx context.Context, feeds []model.Feed, globalLimit, ownerLimit int, process func(context.Context, model.Feed)) {
	if globalLimit <= 0 || ownerLimit <= 0 || ctx.Err() != nil {
		return
	}
	ordered := append([]model.Feed(nil), feeds...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.LastFetchedAt == nil && b.LastFetchedAt != nil {
			return true
		}
		if a.LastFetchedAt != nil && b.LastFetchedAt == nil {
			return false
		}
		if a.LastFetchedAt != nil && !a.LastFetchedAt.Equal(*b.LastFetchedAt) {
			return a.LastFetchedAt.Before(*b.LastFetchedAt)
		}
		return a.ID < b.ID
	})
	queues := make(map[int][]model.Feed)
	owners := []int{}
	for _, feed := range ordered {
		owner := 0
		if feed.OwnerID != nil {
			owner = *feed.OwnerID
		}
		if _, ok := queues[owner]; !ok {
			owners = append(owners, owner)
		}
		queues[owner] = append(queues[owner], feed)
	}
	if globalLimit > len(feeds) {
		globalLimit = len(feeds)
	}
	finished := make(chan int, globalLimit)
	active := make(map[int]int)
	running, next, remaining := 0, 0, len(feeds)
	for remaining > 0 || running > 0 {
		if ctx.Err() != nil {
			break
		}
		launched := false
		if running < globalLimit {
			for offset := 0; offset < len(owners); offset++ {
				index := (next + offset) % len(owners)
				owner := owners[index]
				if len(queues[owner]) == 0 || active[owner] >= ownerLimit {
					continue
				}
				feed := queues[owner][0]
				queues[owner] = queues[owner][1:]
				active[owner]++
				running++
				remaining--
				next = (index + 1) % len(owners)
				go func(owner int, feed model.Feed) {
					defer func() { finished <- owner }()
					if ctx.Err() == nil {
						process(ctx, feed)
					}
				}(owner, feed)
				launched = true
				break
			}
		}
		if launched {
			continue
		}
		select {
		case owner := <-finished:
			active[owner]--
			running--
		case <-ctx.Done():
		}
	}
	// Let admitted tasks release their leases before the next cycle can start.
	for running > 0 {
		<-finished
		running--
	}
}
