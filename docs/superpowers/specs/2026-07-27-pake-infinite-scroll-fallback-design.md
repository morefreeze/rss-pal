# Pake Infinite-Scroll Fallback Design

## Context

The article list loads 20 articles at a time. It currently attaches an
`IntersectionObserver` to the seventh-from-last card and starts the next
request when that card enters a 200 px prefetch margin.

The same production page auto-loads in a regular browser, but the installed
macOS Pake application reaches the bottom with the original 20 articles and
the manual `加载更多` button still visible. Direct inspection of
`/Applications/rsspal.app` confirmed that reaching 100% scroll does not change
the button to `加载中...`. The request is therefore not being started. Pake's
`WKWebView` is not delivering the intersection update expected by the current
single-trigger implementation.

## Goal

Keep early prefetching in browsers while ensuring that scrolling near the end
of the article list also loads the next page in Pake and other embedded
WebViews.

## Non-goals

- Do not remove the manual `加载更多` button.
- Do not add Pake or user-agent detection.
- Do not change pagination size, API parameters, sort behavior, filters, or
  article navigation context.
- Do not rebuild the Pake application. It loads the deployed web application,
  so the frontend deployment is sufficient.

## Chosen Approach

Introduce a small infinite-scroll hook used by `ArticleListPage`.

The hook keeps the existing `IntersectionObserver` as the primary browser
trigger and adds a scroll-based fallback:

1. Observe the existing prefetch card with the existing 200 px margin.
2. Listen passively for window scrolling and, in the capture phase, document
   scrolling so both document and nested WebView scroll areas are covered.
3. Listen for viewport resize.
4. Throttle geometry checks with `requestAnimationFrame`.
5. Trigger when the prefetch card's top is at or above
   `window.innerHeight + 200`.
6. Run the same geometry check when the target card changes after a page is
   appended. This lets a tall viewport keep filling even when no additional
   user scroll occurs.

Both paths call the same guarded trigger. A synchronous in-flight ref prevents
the observer and scroll fallback from starting duplicate requests before React
has committed the `loadingMore` state update.

## Data Flow

`ArticleListPage` continues to own article pagination state and API calls.

1. The page renders the prefetch card and passes its ref, `hasMore`, and the
   asynchronous `loadMore` callback to the hook.
2. An intersection update or a near-viewport geometry check calls the guarded
   trigger.
3. The trigger marks itself in flight before invoking `loadMore`.
4. `loadMore` requests `offset + PAGE_SIZE` and appends the returned articles.
5. The hook releases its lock in `finally`, whether the request succeeds or
   fails.
6. Rendering the new prefetch card re-arms observation and runs an immediate
   geometry check.

## Error Handling

Automatic loading must not create an unhandled promise rejection. The hook
will absorb failures from its automatic trigger after releasing the in-flight
lock. Existing page state remains unchanged, `hasMore` remains true, and the
manual button remains available. A later scroll, intersection update, or
button click can retry.

The manual button continues to call the page's `loadMore` callback directly.

## Testing

Add a focused hook test harness with a real DOM target and controlled geometry.
Cover these behaviors:

- When `IntersectionObserver` never calls back, a scroll event near the
  prefetch boundary starts one load.
- An intersection callback and scroll event in the same turn do not start
  duplicate loads.
- A rejected automatic load releases the guard so a later scroll can retry.
- Disabled/no-more state does not load.
- Unmounting removes listeners, disconnects the observer, and cancels queued
  animation frames.

Run the focused test first, then the full frontend test suites and production
build. Finally deploy the frontend and verify in `/Applications/rsspal.app`
that reaching the prefetch boundary changes the UI to `加载中...` and appends
the next page without clicking the button.

## Alternatives Considered

### Replace the observer with scroll-distance checks

This is compatible but discards the efficient observer path that already works
in regular browsers.

### Detect Pake or its user agent

This produces a smaller conditional change but couples web behavior to wrapper
identity. It can break when Pake or the WebView user agent changes and does not
help other embedded WebViews with the same observer behavior.
