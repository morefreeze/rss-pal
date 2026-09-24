package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/bytedance/rss-pal/internal/taskbudget"
)

func TestSubscriptionNotModifiedAdvancesCheck(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	oldStore, oldPolicies := workerBudgets, workerPolicies
	defer func() { workerBudgets, workerPolicies = oldStore, oldPolicies }()
	workerBudgets = nil
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("If-None-Match") != `"v1"` {
			t.Errorf("missing etag")
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	before := time.Now().UTC().Add(-2 * time.Hour)
	feed := model.Feed{URL: server.URL, FeedType: "rss", ETag: `"v1"`, LastModified: "Wed, 23 Sep 2026 01:00:00 GMT", LastFetchedAt: &before, FetchIntervalMin: 60}
	if err := db.QueryRow(`INSERT INTO feeds(url,title,etag,last_modified,last_fetched_at) VALUES($1,'test',$2,$3,$4) RETURNING id`, feed.URL, feed.ETag, feed.LastModified, before).Scan(&feed.ID); err != nil {
		t.Fatal(err)
	}
	processFeed(context.Background(), repository.NewFeedRepository(db), nil, rss.NewFetcher(""), nil, nil, feed)
	var checked time.Time
	var etag, modified string
	if err := db.QueryRow(`SELECT last_fetched_at,etag,last_modified FROM feeds WHERE id=$1`, feed.ID).Scan(&checked, &etag, &modified); err != nil {
		t.Fatal(err)
	}
	feed.LastFetchedAt = &checked
	if !checked.After(before) || shouldFetch(&feed) {
		t.Fatalf("304 did not advance check: %v", checked)
	}
	if etag != feed.ETag || modified != feed.LastModified {
		t.Fatalf("validators lost: %q %q", etag, modified)
	}
	if shouldFetch(&feed) {
		processFeed(context.Background(), repository.NewFeedRepository(db), nil, rss.NewFetcher(""), nil, nil, feed)
	}
	if calls.Load() != 1 {
		t.Fatalf("checked unchanged feed %d times", calls.Load())
	}
}

func TestSubscriptionFetchIgnoresSaturatedBackfillBudget(t *testing.T) {
	for _, mode := range []string{"concurrency", "daily"} {
		t.Run(mode, func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			oldStore, oldPolicies := workerBudgets, workerPolicies
			defer func() { workerBudgets, workerPolicies = oldStore, oldPolicies }()
			workerBudgets = taskbudget.New(db)
			policy := taskbudget.Policy{Daily: 1, GlobalDaily: 1, Concurrent: 1, GlobalConcurrent: 1, Lease: time.Minute}
			workerPolicies = taskbudget.Policies{"background_fetch": policy, "subscription_fetch": policy}
			release, err := workerBudgets.Acquire(context.Background(), 0, "background_fetch", 1, policy)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if mode == "daily" {
				release()
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNotModified) }))
			defer server.Close()
			feed := model.Feed{URL: server.URL, FeedType: "rss"}
			if err := db.QueryRow(`INSERT INTO feeds(url,title) VALUES($1,'test') RETURNING id`, feed.URL).Scan(&feed.ID); err != nil {
				t.Fatal(err)
			}
			processFeed(context.Background(), repository.NewFeedRepository(db), nil, rss.NewFetcher(""), nil, nil, feed)
			if calls.Load() != 1 {
				t.Fatal("backfill budget blocked subscription request")
			}
			var used, leases int
			if err := db.QueryRow(`SELECT used FROM task_budget_daily WHERE owner_id=0 AND bucket='subscription_fetch'`).Scan(&used); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM task_budget_leases WHERE bucket='subscription_fetch'`).Scan(&leases); err != nil {
				t.Fatal(err)
			}
			if used != 1 || leases != 0 {
				t.Fatalf("usage=%d leases=%d", used, leases)
			}
		})
	}
}

func TestSubscriptionCycleDrainsSameOwnerWithinBudgetConcurrency(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	oldStore, oldPolicies := workerBudgets, workerPolicies
	defer func() { workerBudgets, workerPolicies = oldStore, oldPolicies }()
	workerBudgets = taskbudget.New(db)
	workerPolicies = taskbudget.Policies{"subscription_fetch": {Daily: 100, GlobalDaily: 100, Concurrent: 2, GlobalConcurrent: 5, Lease: time.Minute}}
	var denied, calls, active atomic.Int32
	workerBudgets.SetObserver(func(_ int, _, reason string, _ *time.Time) {
		if reason == "user_concurrency" {
			denied.Add(1)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if active.Add(1) > 2 {
			t.Error("owner concurrency exceeded")
		}
		defer active.Add(-1)
		calls.Add(1)
		time.Sleep(15 * time.Millisecond)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	var owner int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('subscriptions','x') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		if _, err := db.Exec(`INSERT INTO feeds(url,title,owner_id,feed_type,status,fetch_interval_minutes) VALUES($1,'test',$2,'rss','active',60)`, fmt.Sprintf("%s/%d", server.URL, i), owner); err != nil {
			t.Fatal(err)
		}
	}
	feedRepo := repository.NewFeedRepository(db)
	fetchAllFeeds(context.Background(), feedRepo, nil, rss.NewFetcher(""), nil, nil)
	fetchAllFeeds(context.Background(), feedRepo, nil, rss.NewFetcher(""), nil, nil)
	if calls.Load() != 7 || denied.Load() != 0 {
		t.Fatalf("requests=%d concurrency denials=%d", calls.Load(), denied.Load())
	}
}
