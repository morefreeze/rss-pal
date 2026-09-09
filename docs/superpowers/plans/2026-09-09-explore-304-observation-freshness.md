# Explore 304 Observation Freshness Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep unchanged Explore registry entries eligible after a successful conditional `304 Not Modified` synchronization.

**Architecture:** Extend the narrow registry persistence interface with an atomic not-modified operation. Route only the HTTP 304 branch through it; keep 200 parsing/upsert/queue behavior unchanged.

**Tech Stack:** Go 1.25, PostgreSQL 15, `net/http`, repository integration tests with the existing `testdb` helper.

---

### Task 1: Specify the 304 registry behavior

**Files:**
- Modify: `backend/internal/explore/registry_test.go`
- Modify: `backend/internal/repository/explore_registry_test.go`

- [ ] **Step 1: Strengthen the unit test**

Update `TestRegistrySyncDueDoesNotEnqueueOn304` so its store records `RecordNotModified` calls and asserts one not-modified persistence call, zero ordinary success calls, zero candidate upserts, and zero queue items.

- [ ] **Step 2: Add the repository integration test**

Create `TestExploreRegistryRecordNotModifiedRefreshesProviderObservations` with two providers. Model `A+B` at `t1`, a changed response containing only `A` at `t2`, then call `RecordNotModified` at `t3` and assert:

```go
last_seen_at == syncedAt
sourceLastObservedAt == syncedAt
occurrence_count == originalOccurrenceCount
removedSourceLastSeenAt == originalObservedAt
removedSourceLastObservedAt == originalObservedAt
otherProviderLastSeenAt == originalOtherProviderLastSeenAt
last_success_at == syncedAt
consecutive_failures == 0
last_error == nil
```

Add `TestExploreRegistryRecordNotModifiedDoesNotReviveAfterEmptySuccess`. Model `A+B` at `t1`, an empty successful `200` at `t2`, and `304` at `t3`; assert neither observation nor source timestamp advances past `t1`.

- [ ] **Step 3: Verify RED**

Run:

```bash
GOCACHE=/tmp/rss-pal-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/explore ./internal/repository -run 'TestRegistrySyncDueDoesNotEnqueueOn304|TestExploreRegistryRecordNotModifiedRefreshesProviderObservations' -count=1
```

Expected: FAIL because the 304 branch still uses `RecordSuccess` and `ExploreRegistryRepository.RecordNotModified` does not exist.

### Task 2: Implement atomic not-modified persistence

**Files:**
- Modify: `backend/internal/explore/registry.go`
- Modify: `backend/internal/repository/explore_registry.go`

- [ ] **Step 1: Extend the store contract and route 304 responses**

Add this method to `RegistryStore`:

```go
RecordNotModified(providerID int, syncedAt time.Time, etag, lastModified string) error
```

In `syncOne`, call a not-modified helper for `fetched.NotModified`; keep the existing `RecordSuccess` path for parsed `200` responses.

- [ ] **Step 2: Persist provider success and observation freshness atomically**

Implement `ExploreRegistryRepository.RecordNotModified` with `txOrBegin`. Lock the provider row and capture its previous `last_success_at`, update the provider success fields, then refresh only observations written during that previous successful synchronization. Use the refreshed source IDs to advance `recommended_feeds.last_observed_at` in the same statement:

```sql
WITH refreshed AS (
  UPDATE explore_source_observations ...
  WHERE provider_id=$1 AND last_seen_at=previous_success_at
  RETURNING source_id
)
UPDATE recommended_feeds
SET last_observed_at=GREATEST(last_observed_at,$2)
WHERE id IN (SELECT source_id FROM refreshed)
```

Commit only after both writes succeed.

- [ ] **Step 3: Verify GREEN**

Run the focused command from Task 1 and expect both tests to pass.

- [ ] **Step 4: Format and run affected suites**

Run:

```bash
gofmt -w internal/explore/registry.go internal/explore/registry_test.go internal/repository/explore_registry.go internal/repository/explore_registry_test.go
GOCACHE=/tmp/rss-pal-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/explore ./internal/repository -count=1
```

Expected: both packages pass.

### Task 3: Verify and deliver

**Files:**
- Verify only: `backend/...`

- [ ] **Step 1: Run backend verification**

Run full tests, affected race tests, vet, and `git diff --check` with Go 1.25.4. All must exit zero.

- [ ] **Step 2: Commit and push**

Commit the implementation as `fix: preserve explore observations on 304`, push the branch, then merge it to `master` without rewriting history.

- [ ] **Step 3: Deploy only the worker**

On `tencent-rss-pal:/opt/rss-pal`, update to the merged `master`, then run the repository's service-scoped worker build and recreate path. Do not restart API, frontend, RSSHub, PostgreSQL, status monitor, or YouTube POT.

- [ ] **Step 4: Verify production behavior**

Confirm the remote SHA equals merged `origin/master`, `rss-pal-worker-1` is running the rebuilt image, internal/public health is OK, a controlled provider/snapshot cycle produces non-zero candidates, the latest batch is `done`, and `/explore` no longer reports fallback for the affected user state.
