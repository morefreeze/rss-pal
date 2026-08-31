# Subscription Explore Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a first-class Explore experience that recommends healthy public feeds from a continuously refreshed multi-provider registry, presents their recent articles without auto-subscribing, and lets each user explicitly subscribe or give reversible feedback.

**Architecture:** Keep public discovery, validation, queueing, and candidate article cache global; keep batches, ranking reasons, interests, feedback, and events user-scoped behind RLS. A worker synchronizes versioned provider endpoints, enqueues idempotent tasks, leases at most 500 tasks per global run, refreshes the public cache, then publishes deterministic per-user snapshots at six Asia/Shanghai slots. The authenticated API reads the latest complete snapshot and performs transactional promotion from candidate sources/articles into owned formal feeds/articles.

**Tech Stack:** Go 1.24, Gin, PostgreSQL 15, `database/sql`, `gofeed`, React 19, TypeScript, Vite, Vitest, Testing Library, Docker Compose.

---

## Implementation boundaries

- Reading `/explore/articles/:id` never creates a row in `feeds`; subscription only happens through the explicit single or batch subscribe endpoints.
- Candidate source and article data never appears in formal unread counts, saved items, tags, summaries, statistics, OPML exports, or briefing algorithms before subscription.
- Each logical fetch run leases at most 500 tasks globally. `EXPLORE_FETCH_BATCH_LIMIT` defaults to 500 and is clamped to `[1, 500]`; `EXPLORE_FETCH_CONCURRENCY` defaults to 5 and controls only simultaneous network requests.
- Tasks beyond the run quota remain durable `pending` rows. In the 501-task boundary test, exactly 500 rows become `leased` for one `run_id` and exactly one remains `pending`.
- Source validation uses `httpx.ValidateURL` and a redirect-checking safe client, caps provider/feed bodies, and never uses the current permissive `rss.Fetcher` transport for an untrusted discovery URL.
- Public cache rows contain no user ID, private article ID, or discovery trail that can identify a user. User-derived related-site candidates retain only their normalized public URL and generic `related_site` observation.
- The page continues serving the last `done` snapshot while providers, fetches, or a new snapshot fail.
- Existing unrelated work and tests remain untouched.

## Task 1: Add the exploration schema and tenant boundaries

**Files:**

- Create: `backend/migrations/038_subscription_explore.sql`
- Modify: `backend/internal/model/model.go`
- Modify: `backend/internal/repository/rls_migration_test.go`
- Modify: `backend/internal/repository/rls_leak_test.go`
- Create: `backend/internal/repository/explore_migration_test.go`

- [ ] **Step 1: Write failing migration and RLS tests**

Add `TestMigration038_ExploreSchema`, `TestMigration038_FeedURLIsOwnerScoped`, and `TestMigration038_ExploreRunClaimedCountCap`. Assert all nine new/extended structures, foreign keys, checks, unique/partial indexes, and the `claimed_count <= 500` database constraint. Extend the existing RLS migration matrix and private-table leakage matrix with:

```go
"explore_batches", "explore_batch_sources", "explore_feedback", "explore_article_events"
```

The leakage fixtures must seed two users and prove each request transaction sees only its own batch, batch source, feedback, and event.

- [ ] **Step 2: Run the focused tests and confirm the schema is absent**

Run:

```bash
cd backend
go test ./internal/repository -run 'TestMigration038|TestRLS_PrivateTablesAreScoped|TestMigration033_EnablesRLS' -count=1
```

Expected: failure naming the missing migration 038 tables or constraints.

- [ ] **Step 3: Implement migration 038**

Create the following global tables and constraints exactly as defined in the approved design:

- `explore_registry_providers`
- `explore_source_observations`
- `explore_fetch_runs`
- `explore_fetch_queue`
- `explore_articles`

Create these user-scoped tables with `ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY`, and the standard `app_rls_bypass()/app_current_user_id()` policy:

- `explore_batches`
- `explore_batch_sources`
- `explore_feedback`
- `explore_article_events`

Extend `recommended_feeds` with the approved discovery, normalization, validation, health, conditional-request, and observed-at columns. Backfill `normalized_url = lower(trim(url))`, but leave migrated rows in `validation_status='pending'`.

Replace the global `feeds.url` uniqueness with:

```sql
ALTER TABLE feeds DROP CONSTRAINT IF EXISTS feeds_url_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_feeds_owner_url
    ON feeds ((COALESCE(owner_id, 0)), url);
```

Seed enabled default provider rows with stable keys and real endpoints:

```text
plenary-programming-opml -> https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Programming.opml
plenary-tech-opml        -> https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Tech.opml
plenary-webdev-opml      -> https://raw.githubusercontent.com/spians/awesome-RSS-feeds/master/recommended/with_category/Web%20Development.opml
chinese-independent     -> https://raw.githubusercontent.com/timqian/chinese-independent-blogs/master/feed.opml
ooh-recently-added       -> https://ooh.directory/feeds/recently-added.xml
reddit-programming       -> /reddit/subreddit/programming (resolved against RSSHUB_BASE_URL)
awesome-selfhosted       -> https://raw.githubusercontent.com/awesome-selfhosted/awesome-selfhosted/master/README.md
```

Use `ON CONFLICT (provider_key) DO UPDATE` only for versioned endpoint/kind/topic/default interval fields; preserve runtime disable state and sync metadata.

- [ ] **Step 4: Add Go model structs and enums**

Add `ExploreRegistryProvider`, `ExploreSourceObservation`, `ExploreFetchRun`, `ExploreFetchTask`, `ExploreArticle`, `ExploreBatch`, `ExploreBatchSource`, `ExploreFeedback`, and `ExploreArticleEvent` to `model.go`. Use `time.Time`/`*time.Time` consistently with SQL nullability, and constants for every checked status/task/feedback/event value.

- [ ] **Step 5: Run the focused tests**

Run the same focused command and expect PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/migrations/038_subscription_explore.sql backend/internal/model/model.go backend/internal/repository/rls_migration_test.go backend/internal/repository/rls_leak_test.go backend/internal/repository/explore_migration_test.go
git commit -m "feat: add subscription explore schema"
```

## Task 2: Build the persistent queue with a hard global 500-task cap

**Files:**

- Create: `backend/internal/repository/explore_queue.go`
- Create: `backend/internal/repository/explore_queue_test.go`
- Modify: `backend/internal/config/config.go`
- Create: `backend/internal/config/explore_test.go`
- Modify: `.env.example`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Write failing configuration and repository tests**

Cover batch-limit environment values unset, `0`, negative, `1`, `499`, `500`, and `501`; only `1..500` may survive, with every invalid or too-large value resulting in 500. Cover concurrency default 5 and a configured lower positive value.

Repository test cases:

- zero, one, 499, 500, 501, and 1200 pending tasks;
- duplicate enqueue updates priority/`updated_at` without changing original `created_at`;
- two concurrent dispatchers for the same `window_at` combine to at most 500 leases;
- a second call for the same run cannot append more work;
- a different later run can lease tasks that remained pending;
- expired lease recovery remains attached to the original run and does not increment the quota;
- transient failures return to `pending` with exponential `not_before`;
- permanent invalidity transitions to `invalid`.

The 501 assertion must read persisted counts:

```go
if leased != 500 || pending != 1 || run.ClaimedCount != 500 {
    t.Fatalf("leased=%d pending=%d claimed=%d", leased, pending, run.ClaimedCount)
}
```

- [ ] **Step 2: Run tests and confirm failure**

```bash
cd backend
go test ./internal/config ./internal/repository -run 'Explore|Migration038' -count=1
```

- [ ] **Step 3: Implement clamped config**

Add:

```go
type ExploreConfig struct {
    FetchBatchLimit  int
    FetchConcurrency int
}
```

`Load()` reads `EXPLORE_FETCH_BATCH_LIMIT` and `EXPLORE_FETCH_CONCURRENCY`; use a dedicated helper so batch limit always becomes `min(max(value, 1), 500)`, with malformed, non-positive, and over-500 input falling back to 500.

- [ ] **Step 4: Implement queue repository methods**

Implement `Enqueue`, `ClaimRun`, `ListLeased`, `Complete`, `Retry`, `Invalidate`, and `RecoverExpired`. `ClaimRun` must:

1. begin one transaction;
2. call `pg_try_advisory_xact_lock` with a fixed Explore dispatcher key;
3. insert or lock the unique `window_at` run;
4. refuse any fresh claim if `claimed_count > 0`;
5. select tasks using `FOR UPDATE SKIP LOCKED`, priority plus age boost, `LIMIT $batchLimit`;
6. update those rows to `leased`, one `run_id`, one lease owner, and one expiry;
7. set `claimed_count` from the number returned;
8. commit.

Never derive a new allowance from completed, failed, or recovered task counts.

- [ ] **Step 5: Expose deploy configuration**

Document both environment variables in `.env.example` and pass them to the `worker` service in `docker-compose.yml`, defaulting to 500 and 5.

- [ ] **Step 6: Run tests and commit**

```bash
cd backend
go test ./internal/config ./internal/repository -run 'Explore|Migration038' -count=1
cd ..
git add backend/internal/repository/explore_queue.go backend/internal/repository/explore_queue_test.go backend/internal/config/config.go backend/internal/config/explore_test.go .env.example docker-compose.yml
git commit -m "feat: bound explore fetch queue"
```

## Task 3: Add safe provider adapters and discovery normalization

**Files:**

- Create: `backend/internal/explore/provider.go`
- Create: `backend/internal/explore/provider_test.go`
- Create: `backend/internal/explore/opml.go`
- Create: `backend/internal/explore/opml_test.go`
- Create: `backend/internal/explore/directory.go`
- Create: `backend/internal/explore/directory_test.go`
- Create: `backend/internal/explore/markdown.go`
- Create: `backend/internal/explore/markdown_test.go`
- Create: `backend/internal/explore/related.go`
- Create: `backend/internal/explore/related_test.go`
- Create: `backend/internal/explore/registry.go`
- Create: `backend/internal/explore/registry_test.go`

- [ ] **Step 1: Write adapter contract tests with local HTTP fixtures**

Define a provider output as `Candidate{ExternalKey, FeedURL, SiteURL, Title, Topic, Tags}`. Test:

- nested OPML categories and `xmlUrl`/`htmlUrl` parsing;
- ooh.directory Atom/RSS item links converted to site candidates;
- Reddit RSSHub items ignoring Reddit links and aggregating repeated external domains;
- GitHub Awesome Markdown links ignoring GitHub anchors, badges, images, localhost, and duplicate domains;
- RelatedSite `<link rel="alternate" type="application/rss+xml|application/atom+xml">` plus a fixed maximum of 10 external article links;
- query-fragment removal and normalized URL dedupe;
- `If-None-Match`/`If-Modified-Since`, 304 handling, response-size cap, stale-provider status after seven days, and per-provider failure backoff.

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
cd backend
go test ./internal/explore -run 'Provider|OPML|Directory|Markdown|Related|Registry' -count=1
```

- [ ] **Step 3: Implement a shared safe provider client**

Construct it from `httpx.NewClient(timeout)`, call `httpx.ValidateURL` before every initial request, keep redirect validation enabled, and use `io.LimitReader` plus a one-byte overflow probe. Accept only HTTP/HTTPS; no adapter may instantiate `http.DefaultClient` directly.

- [ ] **Step 4: Implement the provider interface and adapters**

`OPMLRegistryAdapter`, `DirectoryAdapter`, `RedditLinkStreamAdapter`, and `GitHubAwesomeAdapter` parse only their format and return candidates. `RelatedSiteDiscoverer` receives already selected public site URLs; it does not receive user IDs or persist private provenance.

Resolve endpoints beginning with `/` against `RSSHUB_BASE_URL`. Preserve provider topic as a generic public tag.

- [ ] **Step 5: Implement registry sync orchestration**

Load enabled due providers, send conditional headers, parse candidates, upsert normalized `recommended_feeds`, upsert source observations, and enqueue `validate_source`. A provider failure updates only that provider and never deletes its prior observations. A 304 updates sync success without re-enqueueing every source.

- [ ] **Step 6: Run tests and commit**

```bash
cd backend
go test ./internal/explore -count=1
cd ..
git add backend/internal/explore
git commit -m "feat: aggregate explore source registries"
```

## Task 4: Validate sources and refresh the public candidate article cache

**Files:**

- Create: `backend/internal/repository/explore_catalog.go`
- Create: `backend/internal/repository/explore_catalog_test.go`
- Create: `backend/internal/explore/fetcher.go`
- Create: `backend/internal/explore/fetcher_test.go`
- Modify: `backend/internal/httpx/client.go`
- Modify: `backend/internal/httpx/client_test.go`
- Modify: `backend/internal/util/urlnorm.go`
- Modify: `backend/internal/util/urlnorm_test.go`

- [ ] **Step 1: Write failing security, validation, and cache tests**

Add table tests for credential-bearing URLs, loopback, RFC1918, link-local, IPv6 local addresses, non-HTTP schemes, unsafe ports, redirect-to-private, and oversized responses. Test feed autodiscovery from safe HTML and direct RSS/Atom without using `rss.NewFetcher`'s permissive client.

Validation fixtures must prove:

- fewer than 2 parseable articles is invalid;
- no article in the last 90 days is invalid for every provider;
- structured provider observation or independent/repeated observation threshold is required;
- transient failure retains last successful cache and degrades health;
- later success restores health;
- successful refresh upserts by `(source_id, normalized_url)` and retains at most 50 recent/30-day rows, with latest 5 retained for valid low-frequency sources.

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
cd backend
go test ./internal/httpx ./internal/util ./internal/explore ./internal/repository -run 'Explore|Validate|Normalize|SSRF|BodyLimit' -count=1
```

- [ ] **Step 3: Add bounded safe fetch primitives**

Extend `httpx` only where needed to expose a safe bounded GET helper that validates the initial URL and every redirect, applies connection/overall timeouts, and reports response overflow distinctly. Keep existing callers compatible.

- [ ] **Step 4: Implement catalog repository**

Implement normalized source upsert, observation merge, due-source lookup, validation state transitions, article upsert, retention cleanup, and last-good-cache queries. All methods accept a `Querier`, allowing worker DB use and API request transactions.

- [ ] **Step 5: Implement queue task handlers**

`validate_source` performs safe direct-feed parsing or HTML autodiscovery, observation confidence checks, the two-article/90-day rule, normalized duplicate merge, and health state update. On success it enqueues `refresh_articles`.

`refresh_articles` uses conditional headers, updates up to the latest 50 candidate articles, stores excerpt/content without AI operations, runs retention, and schedules the next refresh. Classify errors into retryable and terminal invalid outcomes before calling the queue repository.

- [ ] **Step 6: Run tests and commit**

```bash
cd backend
go test ./internal/httpx ./internal/util ./internal/explore ./internal/repository -count=1
cd ..
git add backend/internal/httpx backend/internal/util backend/internal/explore/fetcher.go backend/internal/explore/fetcher_test.go backend/internal/repository/explore_catalog.go backend/internal/repository/explore_catalog_test.go
git commit -m "feat: validate and cache explore sources"
```

## Task 5: Generate deterministic user snapshots on the six approved slots

**Files:**

- Create: `backend/internal/explore/schedule.go`
- Create: `backend/internal/explore/schedule_test.go`
- Create: `backend/internal/explore/profile.go`
- Create: `backend/internal/explore/profile_test.go`
- Create: `backend/internal/explore/ranker.go`
- Create: `backend/internal/explore/ranker_test.go`
- Create: `backend/internal/repository/explore_snapshot.go`
- Create: `backend/internal/repository/explore_snapshot_test.go`

- [ ] **Step 1: Write failing schedule, weighting, and publication tests**

Use an injected clock. Cover 08:00, 11:00, 14:00, 17:00, 20:00, 23:00 Asia/Shanghai; minute-before/after boundaries; late worker startup in the current slot; cross-midnight; and 00:00–08:00 returning no slot. Verify provider sync due time is 30 minutes before each slot.

Build ranking fixtures proving subscriptions outweigh behavior, explicit hide is a hard filter, dampen/boost topics outweigh behavior, health and freshness break deterministic ties, existing visible subscriptions are excluded, cold start works, and at most 12 sources × 5 articles publish.

Repository tests cover `(user_id, slot_at)` idempotency, concurrent generation ownership, pending-to-done atomic publish, failed batch state, and latest-good fallback.

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
cd backend
go test ./internal/explore ./internal/repository -run 'Schedule|Profile|Rank|Snapshot|Batch' -count=1
```

- [ ] **Step 3: Implement source profile and deterministic ranker**

Build tokens/topics/domains from visible formal subscriptions and recent formal article titles/categories/tags. Apply reading/save/like and exploration events only as bounded low-weight corrections. Apply `hide_source`, `dampen_topic`, and `boost_topic` last with explicit priority. Produce a deterministic reason template such as `与你订阅的 {signal} 相关` or cold-start `来自持续更新的 {provider} 目录` when AI is unavailable.

- [ ] **Step 4: Implement snapshot repository**

Create the pending batch only once, compute outside the publish transaction, then atomically insert `explore_batch_sources` and mark `done`. The read path selects the newest `done` batch even if a newer batch is pending/failed. Add cleanup for batches older than 30 days and events older than 180 days.

- [ ] **Step 5: Run tests and commit**

```bash
cd backend
go test ./internal/explore ./internal/repository -count=1
cd ..
git add backend/internal/explore backend/internal/repository/explore_snapshot.go backend/internal/repository/explore_snapshot_test.go
git commit -m "feat: publish personalized explore snapshots"
```

## Task 6: Share article ordering semantics and expose exploration reads/feedback

**Files:**

- Modify: `backend/internal/repository/article.go`
- Modify: `backend/internal/repository/article_test.go`
- Create: `backend/internal/repository/explore.go`
- Create: `backend/internal/repository/explore_test.go`
- Create: `backend/internal/api/explore.go`
- Create: `backend/internal/api/explore_test.go`

- [ ] **Step 1: Write failing ordering, visibility, response, and feedback tests**

Assert the same helper produces these clauses for formal and exploration aliases:

```sql
ORDER BY explore_articles.fetched_at DESC
```

and:

```sql
ORDER BY DATE_TRUNC('day', GREATEST(COALESCE(explore_articles.published_at, explore_articles.fetched_at), explore_articles.fetched_at - INTERVAL '7 days')) DESC,
         COALESCE(explore_articles.published_at, explore_articles.fetched_at) DESC
```

Cover asc/desc, pagination, topic filtering, stable source diversity (no more than two consecutive articles per source while preserving same-source order), source list metadata, old-snapshot fallback flags, and absence of `content` in list JSON.

For detail visibility, prove access only if the source appeared in this user's successful batch within 30 days or is a formal feed visible to the user. Add cross-user and stale-batch denial tests.

Cover feedback create, duplicate idempotency, immediate filtering, allow-listed interest replacement, undo ownership, article-event visibility, and repeated exposure de-noising.

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
cd backend
go test ./internal/repository ./internal/api -run 'Explore|ArticleOrder' -count=1
```

- [ ] **Step 3: Extract the shared order-clause helper**

Move sort SQL construction into an exported or package-shared `ArticleOrderClause(alias string, sort SortMode, dir SortDir) string`; validate the alias against fixed internal constants rather than accepting request input. Change formal `GetAll` to call it, then use it in exploration list queries.

- [ ] **Step 4: Implement repository read/write methods**

Add latest snapshot status, article page, source drawer, visible detail, feedback, interest, and event methods. Every user-scoped call executes through `WithCtx` so RLS middleware owns the transaction.

- [ ] **Step 5: Implement handlers**

Implement:

```text
GET    /api/explore
GET    /api/explore/sources
GET    /api/explore/articles/:id
POST   /api/explore/feedback
DELETE /api/explore/feedback/:id
PUT    /api/explore/interests
POST   /api/explore/articles/:id/events
```

Parse and clamp list pagination to the same limits as `/api/articles`; accept only `published|captured`, `asc|desc`, approved feedback/event enums, and a fixed server-side interest vocabulary.

- [ ] **Step 6: Run tests and commit**

```bash
cd backend
go test ./internal/repository ./internal/api -run 'Explore|ArticleOrder' -count=1
cd ..
git add backend/internal/repository/article.go backend/internal/repository/article_test.go backend/internal/repository/explore.go backend/internal/repository/explore_test.go backend/internal/api/explore.go backend/internal/api/explore_test.go
git commit -m "feat: serve explore articles and feedback"
```

## Task 7: Promote selected candidates into formal subscriptions atomically

**Files:**

- Modify: `backend/internal/repository/feed.go`
- Modify: `backend/internal/repository/feed_test.go`
- Create: `backend/internal/explore/subscribe.go`
- Create: `backend/internal/explore/subscribe_test.go`
- Modify: `backend/internal/api/explore.go`
- Modify: `backend/internal/api/explore_test.go`

- [ ] **Step 1: Write failing transactional subscription tests**

Cover:

- same user + URL is idempotent;
- two users can each create owned feeds for the same URL;
- an existing shared feed is reused;
- source not in the caller's valid recent snapshot is rejected;
- invalid source is rejected;
- candidate articles are upserted into formal `articles` for immediate reading;
- copying does not create explore events, preferences, progress, tags, or summaries;
- a batch of valid sources commits all;
- one invalid source rolls the entire batch back;
- concurrent single-subscribe calls return the same owner-scoped feed.

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
cd backend
go test ./internal/repository ./internal/explore ./internal/api -run 'Subscribe|OwnerScopedFeed' -count=1
```

- [ ] **Step 3: Implement owner-scoped feed lookup/upsert**

Add a repository method that first reuses a visible shared feed, otherwise inserts by `(COALESCE(owner_id, 0), url)` and safely re-reads on conflict. Do not change existing bookmarklet/provider identity indexes.

- [ ] **Step 4: Implement the promotion service**

Accept a `Querier`, validate every source before mutation, open or reuse the outer request transaction via `txOrBegin`, create/reuse all feeds, copy cached article title/URL/content/published/fetched fields with `ON CONFLICT (feed_id, url) DO UPDATE`, and commit only after all requested IDs succeed.

- [ ] **Step 5: Add endpoints and run tests**

Implement:

```text
POST /api/explore/sources/:id/subscribe
POST /api/explore/sources/subscribe-batch
```

Return `{feed_id, created, copied_articles}` for single and one result per requested source for batch.

```bash
cd backend
go test ./internal/repository ./internal/explore ./internal/api -run 'Subscribe|OwnerScopedFeed' -count=1
cd ..
git add backend/internal/repository/feed.go backend/internal/repository/feed_test.go backend/internal/explore/subscribe.go backend/internal/explore/subscribe_test.go backend/internal/api/explore.go backend/internal/api/explore_test.go
git commit -m "feat: subscribe to explore sources"
```

## Task 8: Wire the worker, API server, scheduling, and runtime migration

**Files:**

- Modify: `backend/cmd/worker/main.go`
- Create: `backend/cmd/worker/explore.go`
- Create: `backend/cmd/worker/explore_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Write failing worker orchestration tests**

Use fake registry, queue, task handler, snapshot publisher, and clock interfaces. Assert:

- due provider sync runs 30 minutes before each slot;
- one queue run uses configured limit no greater than 500 and configured concurrency 5 by default;
- multiple worker ticks in the same logical run do not claim again;
- task goroutines never exceed fetch concurrency;
- snapshot generation does not wait for queue drain;
- 00:00–08:00 creates no snapshot;
- one source/provider failure is logged and processing continues;
- old snapshots remain readable after a failed generation.

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
cd backend
go test ./cmd/worker ./cmd/server -run Explore -count=1
```

- [ ] **Step 3: Implement worker orchestration**

Add a once-per-minute `runExploreCycle` guarded independently from the existing feed cycle. Compute canonical provider/run windows and snapshot slots in Asia/Shanghai. Dispatch leased tasks through a semaphore of `FetchConcurrency`, update task outcomes, and publish snapshots for all users without blocking on pending queue length. Include batch/run/provider/source IDs and aggregate discard counts in logs, but no full profile or article content.

- [ ] **Step 4: Wire repositories and protected routes**

Instantiate the exploration repositories/services/handler in `cmd/server/main.go`, and register all Explore routes inside the existing protected `apiGroup` after interest routes.

- [ ] **Step 5: Apply migration 038 to existing Compose volumes**

Change the one-shot `status-migrate` service to run `/migrations/038_subscription_explore.sql` with `ON_ERROR_STOP=1`, rename the comment to describe the current schema migration, and retain dependency gating for API and worker. A fresh database still applies all migrations from `/docker-entrypoint-initdb.d`; migration 038 must therefore be idempotent.

- [ ] **Step 6: Run tests and commit**

```bash
cd backend
go test ./cmd/worker ./cmd/server ./internal/... -count=1
cd ..
git add backend/cmd/worker backend/cmd/server/main.go docker-compose.yml
git commit -m "feat: schedule explore discovery and snapshots"
```

## Task 9: Add frontend API contracts, routes, titles, and navigation

**Files:**

- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/components/Layout.tsx`
- Modify: `frontend/src/components/MobileTabBar.tsx`
- Modify: `frontend/src/components/MoreSheet.tsx`
- Modify: `frontend/src/utils/pageTitle.ts`
- Modify: `frontend/test/MobileTabBar.test.tsx`
- Modify: `frontend/test/RoutePageTitle.test.tsx`
- Create: `frontend/test/ExploreRoutes.test.tsx`

- [ ] **Step 1: Write failing route/navigation/title tests**

Expected mobile primary order:

```text
文章, 网摘, 订阅, 探索, 更多
```

Expected More order begins with `简报`, followed by existing `兴趣, 统计, 设置, 登出`. Desktop navigation includes `🔭 探索`. Assert `/explore` title is `探索 - RSS Pal`, detail uses preview title when present, and authenticated routes render the correct page components.

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
cd frontend
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npx vitest run test/MobileTabBar.test.tsx test/RoutePageTitle.test.tsx test/ExploreRoutes.test.tsx
```

- [ ] **Step 3: Add exact client contracts**

Define list/source/detail/status/feedback/subscribe/event types and functions for every API endpoint. Keep exploration types distinct from formal `Article`; list items must not expose `content` and detail must.

- [ ] **Step 4: Add routes and navigation**

Add lazy or direct routes for `/explore` and `/explore/articles/:id`; insert Explore in desktop nav; replace mobile Briefing with Explore; add Briefing to `MoreSheet`; update active route matching and page title rules.

- [ ] **Step 5: Run tests and commit**

```bash
cd frontend
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npx vitest run test/MobileTabBar.test.tsx test/RoutePageTitle.test.tsx test/ExploreRoutes.test.tsx
cd ..
git add frontend/src/api/client.ts frontend/src/App.tsx frontend/src/components/Layout.tsx frontend/src/components/MobileTabBar.tsx frontend/src/components/MoreSheet.tsx frontend/src/utils/pageTitle.ts frontend/test/MobileTabBar.test.tsx frontend/test/RoutePageTitle.test.tsx frontend/test/ExploreRoutes.test.tsx
git commit -m "feat: add explore navigation and routes"
```

## Task 10: Build the candidate article stream and auto-hidden source drawer

**Files:**

- Create: `frontend/src/pages/ExplorePage.tsx`
- Create: `frontend/src/components/ExploreArticleCard.tsx`
- Create: `frontend/src/components/ExploreSourceDrawer.tsx`
- Create: `frontend/src/hooks/useExploreFeed.ts`
- Modify: `frontend/src/index.css`
- Create: `frontend/test/ExplorePage.test.tsx`
- Create: `frontend/test/ExploreSourceDrawer.test.tsx`
- Create: `frontend/test/useExploreFeed.test.tsx`

- [ ] **Step 1: Write failing stream and drawer tests**

Cover default `published desc`, switching published/captured and asc/desc, topic filter, infinite scroll, stale-request generation isolation, exposure event de-noising, and states for cold start, generating, stale fallback, empty filter, and request failure.

Cover a closed-by-default desktop right drawer and mobile bottom sheet; candidate count handle; outside click; Escape; single subscription; checkbox selection; batch subscription; successful close; error recovery; already-subscribed state; and no `subscribe` request from merely opening an article.

Cover negative feedback optimistic removal, failure rollback, undo restoration to original position, and topic dampening.

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
cd frontend
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npx vitest run test/ExplorePage.test.tsx test/ExploreSourceDrawer.test.tsx test/useExploreFeed.test.tsx
```

- [ ] **Step 3: Implement the feed hook**

Follow `ArticleListPage` and `useInfiniteScrollTrigger` conventions. Reset offset and bump a request-generation ref whenever topic/sort/order changes. Merge pages by article ID and ignore late responses from an older generation. Preserve the original batch order locally for undo.

- [ ] **Step 4: Implement exploration cards and toolbar**

Reuse the visual language of `ArticleCard` without importing formal article actions. Show source, published time, title, excerpt, thumbnail when present, topic, reason, and a menu for hide/dampen. Clicking navigates to `/explore/articles/:id` with `{from:'/explore?...', articlePreview}` and records click, not subscribe.

- [ ] **Step 5: Implement responsive source drawer**

Use `useBreakpoint`: desktop overlay from the right, mobile overlay from the bottom. Keep it closed initially and after successful subscription. Show health, topic, reason, recent article count, checkbox, single button, and `订阅已选 N 个`; do not add Subscribe All.

- [ ] **Step 6: Run tests and commit**

```bash
cd frontend
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npx vitest run test/ExplorePage.test.tsx test/ExploreSourceDrawer.test.tsx test/useExploreFeed.test.tsx
cd ..
git add frontend/src/pages/ExplorePage.tsx frontend/src/components/ExploreArticleCard.tsx frontend/src/components/ExploreSourceDrawer.tsx frontend/src/hooks/useExploreFeed.ts frontend/src/index.css frontend/test/ExplorePage.test.tsx frontend/test/ExploreSourceDrawer.test.tsx frontend/test/useExploreFeed.test.tsx
git commit -m "feat: build explore article stream"
```

## Task 11: Add safe full candidate-article reading without formal actions

**Files:**

- Create: `frontend/src/pages/ExploreArticlePage.tsx`
- Create: `frontend/test/ExploreArticlePage.test.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/utils/pageTitle.ts`

- [ ] **Step 1: Write failing detail tests**

Assert detail fetch, Markdown rendering, original link, source metadata, back-to-Explore path, loading/error/forbidden states, top and bottom Subscribe buttons, subscribed state, click/completed-read events, and reading settings. Explicitly assert there are no save/like/dislike/tag/share/summary/progress calls or controls.

- [ ] **Step 2: Run the focused test and confirm failure**

```bash
cd frontend
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npx vitest run test/ExploreArticlePage.test.tsx
```

- [ ] **Step 3: Implement the detail page**

Use `MarkdownArticle`, `CodeWrapContext`, `useReaderSettings`, and the metadata styling from `ReadingLayout`, but create an Explore-specific wrapper so no formal `article_id` action is accidentally mounted. The subscribe handler calls only the explicit source endpoint. Record `completed_read` once when the user reaches the completion threshold; it remains an Explore event only.

- [ ] **Step 4: Run tests and commit**

```bash
cd frontend
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npx vitest run test/ExploreArticlePage.test.tsx
cd ..
git add frontend/src/pages/ExploreArticlePage.tsx frontend/test/ExploreArticlePage.test.tsx frontend/src/App.tsx frontend/src/utils/pageTitle.ts
git commit -m "feat: read explore articles safely"
```

## Task 12: Full verification, review, integration, and Tencent delivery

**Files:**

- Modify as required by review findings only.
- Verify: `docs/superpowers/specs/2026-08-31-subscription-explore-design.md`
- Verify: `docs/superpowers/plans/2026-08-31-subscription-explore.md`

- [ ] **Step 1: Run backend formatting and full tests**

```bash
cd backend
gofmt -w cmd internal
go test ./... -count=1
```

Expected: all packages PASS, including PostgreSQL migration/RLS, queue 501 boundary, concurrency, API authorization, and worker schedule tests.

- [ ] **Step 2: Run frontend checks and production build**

```bash
cd frontend
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm run check
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm run build
```

Expected: Vitest, legacy tests, TypeScript checks, and Vite production build pass. Existing chunk-size warnings are acceptable; new errors are not.

- [ ] **Step 3: Run local database/API smoke tests**

Bring up the local stack with migration 038. Seed or sync a fixture provider, enqueue 501 candidate tasks, trigger one run, and query the DB to prove 500 leased/processed and one still pending. Log in through the local API and verify:

- `/api/explore` returns candidate articles without content;
- detail returns content and does not create a feed;
- single subscribe creates/reuses one owner-scoped feed and copies cached articles;
- batch failure rolls back;
- cross-user detail/feedback IDs return forbidden/not found;
- an old done snapshot remains served while a newer batch is failed.

- [ ] **Step 4: Review against the approved specification**

Invoke `superpowers:requesting-code-review`. Resolve every high/medium correctness, security, multi-tenant, queue-cap, transaction, and UI-boundary finding. Re-run all affected focused tests, then repeat Steps 1–3.

- [ ] **Step 5: Commit final review fixes and verify clean branch**

```bash
git status --short
git log --oneline --decorate -12
git diff master...HEAD --check
```

Commit only feature/review files; do not include unrelated user changes.

- [ ] **Step 6: Merge to master and push**

Invoke `superpowers:finishing-a-development-branch`. Merge `codex/subscription-explore` into current `master`, resolve only in-scope conflicts, re-run backend full tests and frontend check/build on the merged revision, then:

```bash
git push origin master
```

Record the exact pushed commit and verify `origin/master` resolves to it.

- [ ] **Step 7: Wait for and verify Tencent deployment**

Treat `tx` as `tencent-rss-pal`. Wait for the deployment process that consumes `origin/master`, then verify all of:

- remote checkout revision equals the pushed master commit;
- migration 038 succeeded and the one-shot migrate container exited 0;
- API, worker, frontend, RSSHub, PostgreSQL, status monitor, and other configured runtime services are running;
- direct API health and `https://rss.morefreeze.top/api/health` succeed;
- authenticated public `/api/explore` works for a real user;
- the public frontend bundle contains the Explore route/navigation and the page loads;
- worker logs show one bounded Explore run with claimed count no greater than 500 and no private profile/body logging.

If deployment fails, diagnose from the exact remote revision, container state, migration output, and logs; fix in scope, re-run verification, push a new master commit, and re-verify the deployed revision.

## Execution Routing

This is a large implementation with independently reviewable schema/queue, discovery/fetching, ranking/API, and frontend slices. Execute it with `superpowers:subagent-driven-development`, one task at a time with spec-compliance and code-quality review gates. Tasks share the same branch, so never run agents that edit overlapping files concurrently. Keep the persistent queue and RLS work ahead of every consumer/API/UI task.
