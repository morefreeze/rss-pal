# Weak Network Performance Optimization — Design

**Date:** 2026-05-25
**Status:** Approved, ready for implementation

## Problem

When the user accesses RSS Pal over their domain (rather than localhost), the article list page and article detail page take 3–5 seconds to load. The latency is dominated by the network path, not by application logic, but multiple compounding inefficiencies make it worse than necessary.

## Goal

Cut first-screen time from 3–5 s to roughly 600 ms–1.2 s on a 4G-throttled connection (~100 KB/s downlink), without regressing freshness on the article list.

Success criteria (Chrome DevTools, "Slow 4G" profile):
- Article list page: TTI < 1.5 s.
- Article detail page: LCP < 1.2 s on first visit, < 300 ms on a cached revisit.
- List response body gzipped: < 10 KB for a typical 20-item page (today: ~100 KB).
- Repeat detail visits within 5 min return from disk cache (no network round-trip).

## Root Causes (from code survey)

1. **Nginx has no gzip.** `frontend/nginx.conf` serves JS/CSS/JSON uncompressed; the SPA bundle pays full byte cost.
2. **List API returns full `content` and `summary_detailed` for every row.** Frontend list only renders `summary_brief` (first 120 chars). Dropping the unused fields removes ~80–90 % of the body.
3. **No HTTP caching.** Every request to `/api/articles` and `/api/articles/:id` is fully revalidated end-to-end; no `ETag`, no `Cache-Control`.
4. **Serial fetch waterfall.** `ArticleListPage` fires `getFeeds()` then `getArticles()` in dependent `useEffect`s. First screen = 2× RTT.
5. **Missing DB indexes.** `articles` has no `(feed_id, published_at DESC)` / `(feed_id, fetched_at DESC)` composite index. Sort + filter goes through a sequential-ish scan as the table grows.
6. **No axios timeout or retry.** A single dropped packet on weak 4G stalls the request indefinitely, then surfaces as a perceived "hang."
7. **No skeleton screens.** While the list is loading the page is fully blank, exaggerating the perceived wait.

## Scope

In scope:
- Network/transport optimizations (gzip, ETag, Cache-Control).
- API payload trimming (list endpoint only).
- Request orchestration (parallelization, timeout, retry).
- DB composite indexes for list query.
- Skeleton screens for list and detail first-screen.

Out of scope (explicitly deferred):
- Service Worker / IndexedDB offline cache.
- Bundle-size reduction (code splitting beyond what Vite already does).
- Recommended-articles endpoint (small N, not on the critical path).
- Other API endpoints (tags sidebar, feed list) — already small.

## Design

### A. Nginx (`frontend/nginx.conf`)

Enable gzip for JS/CSS/HTML/JSON/SVG:

```
gzip on;
gzip_vary on;
gzip_min_length 512;
gzip_comp_level 6;
gzip_types application/javascript text/css application/json text/html text/xml image/svg+xml application/xml;
```

HTTP/2 is already on; the 1-year `Cache-Control` on hashed assets stays. No other changes.

### B. Gin gzip middleware (`backend/cmd/server/main.go`)

Defensive: when the API is hit directly (no nginx in path) responses should still be compressed.

- Add `github.com/gin-contrib/gzip` middleware, level=5, `MinContentLength=512`.
- Place **before** auth middleware so it covers all routes.
- Exclude already-compressed paths (images, the streaming summary endpoint that already sets `Cache-Control: no-cache`).

### C. List API payload trim (`backend/internal/api/article.go`)

`GET /api/articles` currently serializes the full `model.Article`. Introduce a thin list DTO:

```go
type ArticleListItem struct {
    ID                  int64           `json:"id"`
    FeedID              int64           `json:"feed_id"`
    FeedTitle           string          `json:"feed_title,omitempty"`
    Title               string          `json:"title"`
    URL                 string          `json:"url"`
    PublishedAt         *time.Time      `json:"published_at"`
    SummaryBrief        string          `json:"summary_brief"`
    FetchedAt           time.Time       `json:"fetched_at"`
    WordCount           *int            `json:"word_count,omitempty"`
    ReadingMinutes      *int            `json:"reading_minutes,omitempty"`
    IsRead              *bool           `json:"is_read,omitempty"`
    MediaURL            string          `json:"media_url,omitempty"`
    MediaType           string          `json:"media_type,omitempty"`
    MediaDurationSec    *int            `json:"media_duration_seconds,omitempty"`
    IsLinkSet           *bool           `json:"is_link_set,omitempty"`
    LinksExtendable     *bool           `json:"links_extendable,omitempty"`
    LinkSetSuggested    *bool           `json:"link_set_suggested,omitempty"`
    ParentArticleID     *int64          `json:"parent_article_id,omitempty"`
    ProcessingState     string          `json:"processing_state,omitempty"`
    PrerankScore        *float64        `json:"prerank_score,omitempty"`
    EditorNote          string          `json:"editor_note,omitempty"`
    ManualTags          []model.UserTag `json:"manual_tags"`
}
```

`content` and `summary_detailed` are gone. SQL query can still select them in the short term (just unmap them), but we should also drop them from the SELECT list to save DB→app bandwidth and Go allocations.

Frontend mirror: in `frontend/src/api/client.ts` split `Article` into `ArticleListItem` (subset, no content) and keep `Article` for the detail endpoint. `getArticles()` returns `ArticleListItem[]`; `getArticle()` returns the full `Article`. TypeScript will surface every consumer of `.content` on a list item — verify only `ArticlePage` reads `.content`.

### D. ETag + Cache-Control

**List `GET /api/articles`:**
- Compute weak ETag = `W/"<hex hash of: max(fetched_at) || count || first_id || last_id || query_signature>"`.
  - Cheap: one extra `SELECT MAX(fetched_at), COUNT(*), MIN(id), MAX(id) FROM ...` *only when needed for hashing*; or compute directly from the page result set after the main query (no extra DB round trip).
- If `If-None-Match` matches, return `304 Not Modified` with no body.
- `Cache-Control: private, no-cache` (= must revalidate every time, but 304 lets us skip the body).

**Detail `GET /api/articles/:id`:**
- ETag = `W/"<hex hash of: fetched_at || len(summary_detailed) || len(content)>"`.
- `Cache-Control: private, max-age=300, stale-while-revalidate=600`. Browser serves from cache for 5 min, then revalidates in the background up to another 10 min.
- Streaming summary endpoint stays `Cache-Control: no-cache` (already set).

### E. Database indexes (`backend/migrations/027_perf_indexes.sql`)

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_articles_feed_published
  ON articles (feed_id, published_at DESC NULLS LAST);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_articles_feed_fetched
  ON articles (feed_id, fetched_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_articles_published
  ON articles (published_at DESC NULLS LAST);
```

All `CONCURRENTLY` so writes are not blocked. Must be run outside a transaction — the file uses single statements separated by blank lines, and the README/migration runner already supports this pattern.

**Apply requires manual `psql` execution** on the user's existing DB (per CLAUDE.md: `docker-entrypoint-initdb.d` only runs on first init). The PR description will spell out the exact command.

### F. Frontend request orchestration

`ArticleListPage` (`frontend/src/pages/ArticleListPage.tsx`):
- Replace the two dependent `useEffect`s for `getFeeds()` and `getArticles()` with a single `Promise.all([getFeeds(), getArticles(initialQuery)])` on mount. Both results land together; rendering can start when articles arrive (feeds are needed only for the title labels and sidebar — those can render with placeholder names if late).

`api/client.ts`:
- `axios.create({ ..., timeout: 10000 })`.
- Response interceptor: on `code === 'ECONNABORTED' || !response`, if `config.method === 'get'` and `config.__retryCount` undefined or 0, increment, wait 500 ms, retry once. Never retry non-GET — risk of duplicate writes.

### G. Skeleton screens

- `ArticleListSkeleton` component: render 8 rows of the same structure as `ArticleRow` (title bar 60 % width, two short metadata bars, one pulse-animated). Show when `loading && articles.length === 0` (first-load only — pagination uses the existing spinner).
- `ArticleDetailSkeleton` component: title bar + 5 paragraph bars. Show when `loading && !article`.
- Pure CSS animation (`@keyframes pulse`). No image, no JS.

## Architecture / Flow

```
Browser
  ├─ Initial HTML (cached, instant)
  ├─ JS bundle (gzipped, ~30% of current bytes, HTTP/2 multiplexed)
  └─ on mount: Promise.all([
        GET /api/feeds        (300ms RTT, small body, 304 on revisit)
        GET /api/articles?... (300ms RTT, ~8KB gzipped, 304 on revisit)
     ])
  → first paint with skeleton (within ~200ms after JS parse)
  → render real rows when articles arrives (~600ms after mount)

Article detail navigation
  └─ GET /api/articles/:id
       1st visit: 300ms RTT + ~5KB gzipped body
       Within 5 min: served from disk cache, 0 RTT
       After 5 min: stale-while-revalidate, instant render + background fetch
```

## Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| ETag hash collision across distinct list contents | User stops seeing new articles | Hash includes max(fetched_at), count, first+last id, and a signature of query params. Only on GET. |
| Other frontend callers depend on `content`/`summary_detailed` from list | TS compile / runtime errors | Split type → TS compiler surfaces all consumers. Verify only `ArticlePage` reads `.content`. |
| Double-gzip (Gin + nginx) wastes CPU, may corrupt | Broken responses | Nginx by default does not gzip upstream `Content-Encoding: gzip`. Gin middleware sets the header; nginx passes through. |
| Migration locks the table | Worker write stalls | All `CREATE INDEX CONCURRENTLY`. Doc tells user to run manually with `psql`. |
| axios retry replays mutations | Duplicated POSTs | Retry strictly gated to `method === 'get'`. |
| `max-age=300` hides updated detail | User sees stale article | `stale-while-revalidate` covers background refresh; user-initiated reload sends `Cache-Control: no-cache`. Worker-side article updates are rare enough that 5 min is acceptable. |
| Removing `content` from list query breaks an obscure consumer | 500s | Grep all callers of `getArticles` server-side and `client.getArticles` frontend-side before merging. |

## Testing

**Automated**
- `go test ./internal/api/...`
  - List handler: response JSON has no `content` / `summary_detailed` keys.
  - List handler: second request with matching `If-None-Match` returns 304 with empty body.
  - Detail handler: ETag round-trip; Cache-Control header present and well-formed.
- `npm run build` in `frontend/` — TypeScript split forces every list consumer to compile against `ArticleListItem`.

**Manual (verified by user)**
- `docker-compose up -d --build` then DevTools throttling "Slow 4G" + 100 KB/s.
- Compare before/after numbers for TTI and LCP on list and detail.
- Confirm list body is ~10 KB gzipped.
- Revisit detail page → "from disk cache" in Network tab.
- Disconnect network mid-request and reconnect → axios retry succeeds.
- Skeleton visible within 200 ms after the browser has the JS bundle.

**Regression**
- Infinite scroll still works (PAGE_SIZE=20, IntersectionObserver).
- Tag sidebar, recommendations, save/hide/like all unaffected.
- After worker fetches new articles, refreshing the list shows them (ETag invalidates because max(fetched_at) changes).

## Implementation Order

One PR, eight commits, in this order so each is independently revertible:

1. `nginx.conf` gzip block.
2. Backend Gin gzip middleware.
3. `ArticleListItem` type split: backend DTO + frontend TS + handler change.
4. List ETag + Cache-Control.
5. Detail ETag + Cache-Control.
6. axios timeout + GET-only retry.
7. `ArticleListPage` parallel fetch (Promise.all).
8. Skeleton components for list + detail.
9. Migration `027_perf_indexes.sql` (separate file; PR description includes the `psql` apply command).

## Non-Goals / Deferred

- Service Worker / IndexedDB.
- Backend response streaming (chunked) — not justified for the body sizes after trim.
- Code splitting beyond Vite default.
- Recommended-articles endpoint changes.
