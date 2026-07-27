# Article Detail Prefetch and Instant Render — Design

**Date:** 2026-07-27
**Status:** Approved for implementation

## Problem

Opening an article from the list at `rss.morefreeze.top` currently leaves the
reader on a detail skeleton for 3–5 seconds.

Production evidence separates application time from network time:

- Recent `GET /api/articles/:id` requests complete inside the API container in
  5–15 ms.
- The OCI host is healthy and has no meaningful CPU or memory pressure.
- The current client-to-OCI path measured about 143 ms RTT and 30% packet loss.
- A new TLS connection took 1.4–2.6 seconds, while a reused connection still
  took about 1.3 seconds to return a 34-byte health response.
- Article detail responses seen in nginx access logs were only 2.7–22.7 KB, so
  server compute and response size are not the dominant delay.
- `ArticlePage` renders only a skeleton until `GET /api/articles/:id` finishes.
  The response uses `Cache-Control: private, no-cache`, so every navigation
  revalidates before any article content is shown.

The first phase must improve perceived and measured article-entry latency
without changing DNS, CDN, TLS termination, or the OCI host. Public-network
changes are a separate second phase after this deployment is measured.

## Goals

- Show full article content within 100 ms when a list-initiated prefetch or
  session-memory cache is available.
- Never show a blank detail card while an uncached request crosses the network:
  immediately show the title, source metadata, and brief summary already
  present in the list item.
- Preserve the existing freshness contract by revalidating every article
  detail navigation in the background.
- Avoid duplicate requests when list prefetch and detail navigation overlap.
- Bound speculative traffic and memory use.
- Measure cold, prefetched, and repeat navigation on the deployed production
  site using the same client and representative article set.

## Non-Goals

- Changing DNS, adding a CDN, moving the OCI host, or enabling HTTP/2/HTTP/3.
- Changing the backend article-detail response or its `private, no-cache`
  policy.
- Persisting article bodies across browser sessions with IndexedDB, a service
  worker, or local storage.
- Prefetching every article returned by infinite scrolling.
- Optimizing image proxying or image formats in this phase.

## Chosen Approach

Use a small session-memory detail cache with in-flight request coalescing,
controlled list prefetch, immediate cached rendering, and mandatory background
revalidation.

This is preferred over restoring a positive HTTP `max-age`, which previously
made refreshed article content and private state stale, and over showing only a
list-item shell, which improves feedback but leaves the actual body blocked on
the lossy network.

## Components

### Detail request cache

Add `frontend/src/api/articleDetailCache.ts` as the single owner of article
detail reads.

It stores at most 30 entries in an LRU-style `Map<number, CacheEntry>`. Each
entry contains the complete `ArticleDetailResponse` and the time it was
received. Entries are soft-fresh for five minutes. A soft-expired entry remains
available for immediate rendering, but navigation always starts a fresh
revalidation.

The module also stores one in-flight promise per article ID. Prefetch and
navigation for the same ID share that promise. Failed requests are never
cached, and their in-flight entry is removed so a later navigation can retry.

Public operations:

- `peekArticleDetail(id)` returns cached data without network I/O and updates
  recency.
- `fetchArticleDetail(id)` returns the shared network promise and updates the
  cache on success.
- `prefetchArticleDetail(id)` calls the same fetch path and converts failure
  into a silent result suitable for speculative work.
- `putArticleDetail(response)` synchronizes the cache after a successful
  detail refresh or local mutation.
- `invalidateArticleDetail(id)` removes one entry when a mutation cannot be
  merged safely.
- `resetArticleDetailCache()` exists for logout and deterministic tests.

### Controlled list prefetch

`ArticleListPage` schedules prefetch only after the first list page has rendered.
It selects the first six currently visible article IDs and processes them with
at most two concurrent requests. Work starts through
`requestIdleCallback(..., {timeout: 1000})`, with a zero-delay timer fallback
for browsers that do not implement the API.

Each article card also requests a priority prefetch on pointer enter, focus, or
touch start. These event paths share the same in-flight promise as idle
prefetch, so interaction never creates a duplicate request.

Pagination does not automatically prefetch every appended result. Newly
visible items become eligible through the same bounded visible-item policy.

### Navigation preview handoff

When a list item is opened, navigation state carries its lean
`ArticleListItem`. `ArticlePage` accepts this only when its ID matches the route.
The preview is a fallback for an uncached navigation and displays title,
feed/source, publication time, and `summary_brief` while the full response is
pending. It never pretends that article content has loaded.

Direct links, browser reloads, daily/weekly pages, and other navigation sources
continue to work without preview state.

### Article-page stale-while-revalidate flow

On each route ID:

1. Read `peekArticleDetail(id)` synchronously.
2. If present, render the cached full response immediately and mark it as
   revalidating rather than returning to the full-page skeleton.
3. If absent, render the matching list preview when available; otherwise show
   the existing detail skeleton.
4. Start `fetchArticleDetail(id)` unconditionally.
5. Apply the fresh response only if its ID still matches the active route.
6. On refresh failure, keep already rendered cached content and show a
   non-blocking retry message. If neither cached content nor preview exists,
   retain the existing blocking error state and expose a retry button.

This preserves current correctness: progress, signals, hidden state, link-set
children, and article content all converge to the server response on every
entry. Cached private state can be visible only during the network delay.

### Mutation coherence

Mutations that already receive enough response data to update the active page
write the merged response back through `putArticleDetail`. Mutations that do
not return a complete safe representation call `invalidateArticleDetail(id)`.
At minimum, hide/unhide, link-set refresh, summary/content replacement, and
logout invalidate affected entries. Reading-progress writes may update the
cached progress object after a successful response.

## Race and Failure Handling

- The in-flight registry guarantees one detail request per article ID.
- `ArticlePage` associates every load with the route ID that started it and
  ignores late responses for an older route.
- Speculative failures are silent and retryable.
- A failed background revalidation never removes already rendered content.
- Cache eviction affects performance only; it cannot affect correctness.
- Missing or malformed navigation preview state is ignored.
- Logout resets the cache so private data cannot appear for a later user in
  the same tab.

## Testing

### Automated

Use Vitest with mocked time and a controllable request function:

- Cache hit returns synchronously.
- Concurrent prefetch and navigation make one network call.
- Success caches the response and enforces the 30-entry limit.
- Soft-expired data remains readable while a fresh request runs.
- Failure clears the in-flight entry and a later call retries.
- Invalidation and reset remove cached private data.

Use React Testing Library for the page/list integration:

- A cached article body renders before the revalidation promise resolves.
- An uncached list navigation renders preview title and brief summary.
- A late response for article A cannot overwrite article B after navigation.
- Idle prefetch is capped at two concurrent requests and the selected visible
  item limit.
- Pointer/focus/touch prefetch reuses an existing request.

Run all existing frontend tests and the production TypeScript/Vite build.
Backend behavior is unchanged, but run the full Go suite before deployment.

### Production performance verification

Measure at least five samples for the same three representative articles and
report median plus range:

1. **Cold:** reload the list, immediately open an article before prefetch
   completes.
2. **Prefetched:** reload the list, wait for idle prefetch, then open a visible
   article.
3. **Repeat:** return to the list and reopen the same article.

Record:

- click-to-preview time;
- click-to-full-content time;
- whether the detail request was new, shared in-flight, or cache-seeded;
- browser request duration;
- nginx status/body bytes;
- API handler duration.

Success requires prefetched and repeat full-content medians below 100 ms.
Cold entry may still reflect the lossy network, but must show the preview
within one animation frame. Compare results with the pre-change 3–5 second
user-visible baseline before starting the infrastructure phase.

## Deployment

After automated verification:

1. Integrate the feature branch into `master`.
2. Push `origin/master`.
3. Deploy on `oci-rss-pal` with `/opt/rss-pal/scripts/auto_deploy.sh`.
4. Verify container health and `/api/health`.
5. Run the production performance matrix above.

Only after those results are recorded should phase two evaluate the public
entry path, beginning with HTTP/2/HTTP/3 or a nearer/CDN edge.
