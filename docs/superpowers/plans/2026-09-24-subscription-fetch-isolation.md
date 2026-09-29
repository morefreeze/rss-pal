# Subscription Fetch Isolation Implementation Plan

> **For agentic workers:** Inline execution with executing-plans; changes share the worker entry point and database budget contract.

**Goal:** Keep subscribed feed polling independent from Explore and content backfill.
**Architecture:** Add a separate subscription budget, bounded fair per-owner dispatch, and two independent non-overlapping periodic loops. Treat HTTP 304 as a successful check without changing cached validators.
**Tech Stack:** Go, PostgreSQL, Docker Compose, React monitoring labels.

## Task 1: Budget and successful-check semantics

- [x] Add `backend/cmd/worker/subscription_test.go`: real HTTP 304 + PostgreSQL `processFeed` test; assert `last_fetched_at` advances, validators survive, and `shouldFetch` becomes false. Saturate the background budget and assert subscription still checks its feed.
- [x] Add `backend/internal/taskbudget/subscription_test.go`: defaults, env overrides, invalid configuration and independent counters/leases.
- [x] Run these tests first and retain failing output in `/tmp/rss-subscription-red.log`.
- [x] In `policy.go`, register `subscription_fetch` with `{500, 5000, 2, 5}` and administrator daily limit 2000. Read `TASK_SUBSCRIPTION_FETCH_*` independently from existing background settings.
- [x] In `processFeed`, call `admitBackground(ctx, "subscription_fetch")`. On nil result use `feedRepo.UpdateFetchInfo(feed.ID, feed.ETag, feed.LastModified, time.Now())` before returning.

## Task 2: Bounded scheduling and observability

- [x] Add callback-based behavioral tests for scheduling: blocked backfill does not prevent repeated subscription checks; each loop is non-overlapping and cancellation waits for callbacks. Add dispatcher tests for owner fairness, global/owner bounds, queued work and cancellation.
- [x] Add `subscription.go` with `runWorkerLoops(ctx, interval, subscriptions, backfill)` and `dispatchSubscriptionFeeds(ctx, feeds, globalLimit, ownerLimit, process)`.
- [x] Wire `main` to independent loops. Keep `runFetchCycle` as the backfill callback without `fetchAllFeeds`; run subscription polling from its own callback. Dispatch oldest-due feeds fairly across owners within policy limits.
- [x] Add the new task category to opsmonitor validation and frontend labels; add all five subscription environment values to Compose and `.env.example`.
- [x] Verify targeted tests, then worker/budget/monitor tests under the race detector and `go vet`. Use isolated PostgreSQL at port 55439; investigate any skips instead of claiming database coverage.
- [x] Review the final diff, update this checklist and report local verification and deployment boundary. Leave existing user edits untouched.

Verification command (from backend):
```sh
TEST_DB_URL='postgres://postgres:postgres@127.0.0.1:55439/rsspal_test?sslmode=disable' /Users/bytedance/sdk/go1.25.3/bin/go test -race ./cmd/worker ./internal/taskbudget ./internal/opsmonitor -count=1
```

## Verification result

- Real PostgreSQL + HTTP regression failures recorded before implementation: 304 timestamp, saturated background quota, missing policy and dropped monitoring event.
- `go test -race ./cmd/worker ./internal/taskbudget ./internal/opsmonitor -count=1`: all three packages passed with PostgreSQL at port 55439.
- `go vet ./cmd/worker ./internal/taskbudget ./internal/opsmonitor`: passed.
- `vitest run test/AdminMonitoringPage.test.tsx`: 7 passed; existing Vite deprecation warnings only.
- `tsc --noEmit`: passed.
- No production deployment in this change.
