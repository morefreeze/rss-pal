# Article Page Navigation — Entry-Path Tracking

**Date:** 2026-05-07
**Branch:** `feature/insights-full` (extends PR #5)

## Problem

1. **返回 button uses `navigate(-1)`.** After reading 3+ articles via prev/next, history is polluted: 返回 lands on the previous article instead of the source list.
2. **Direct URL entry has no defined "back" behavior.** `navigate(-1)` from a fresh tab goes to `about:blank` or unrelated history — not a usable place.
3. **Prev/next list is unaware of entry context.** Articles entered from the new `RecommendationsCard` reuse a stale `articleNavList` from a prior list session.
4. **`articleNavList` stores all loaded ids** (potentially hundreds), unnecessary for prev/next.

## Goal

返回 always lands on the page the user came from (list, /insights, etc.), with deterministic fallback for direct URL entry. Prev/next is governed by the **list at time of entry**, capped to ±50 around the clicked article.

## Decisions

| # | Decision |
|---|----------|
| Q1 | From a non-list entry (recommendations, share, direct URL): 返回 goes to the entry page; prev/next is hidden. |
| -- | Entry path is tracked **explicitly** via React Router `location.state.from`, with `sessionStorage.articleEntryPath` as a refresh-survivable backup. `navigate(-1)` is **not** used (unreliable on direct URL entry). |
| -- | Prev/next uses `navigate(url, { replace: true, state: { from: entryPath } })` so the browser's native back also lands on the entry page, and `state.from` survives prev/next chains. |
| -- | `articleNavList` is capped to ±50 around the clicked article (max 101 ids). |

## Architecture

```
List click   → state.from='/articles', sessionStorage.entryPath='/articles', navList=[±50 ids]
Rec click    → state.from='/insights', sessionStorage.entryPath='/insights', navList cleared
Direct URL   → no state, sessionStorage.entryPath may be stale or empty → fallback '/articles'

ArticlePage:
  entryPath = location.state.from ?? sessionStorage.articleEntryPath ?? '/articles'
  prev/next: navigate(url, { replace: true, state: { from: entryPath } })
  返回:      navigate(entryPath)
```

## Frontend Changes

### `frontend/src/pages/ArticleListPage.tsx`

In the click handler that already sets `articleNavList`:

```ts
const handleArticleClick = (id: number) => {
  const ids = articles.map(a => a.id)
  const i = ids.indexOf(id)
  const start = Math.max(0, i - 50)
  const end = Math.min(ids.length, i + 51)
  sessionStorage.setItem('articleNavList', JSON.stringify(ids.slice(start, end)))
  sessionStorage.setItem('articleListScroll', String(window.scrollY))
  sessionStorage.setItem('articleEntryPath', '/articles')
  navigate(`/articles/${id}`, { state: { from: '/articles' } })
}
```

(The existing inline `onClick` is refactored into this single handler.)

### `frontend/src/components/RecommendationsCard.tsx`

```tsx
onClick={() => {
  sessionStorage.removeItem('articleNavList')
  sessionStorage.setItem('articleEntryPath', '/insights')
  navigate(`/articles/${a.article_id}`, { state: { from: '/insights' } })
}}
```

### `frontend/src/pages/ArticlePage.tsx`

Add at the top of the component:

```ts
const location = useLocation()
const entryPath =
  (location.state as { from?: string } | null)?.from
  ?? sessionStorage.getItem('articleEntryPath')
  ?? '/articles'

const handleBack = () => navigate(entryPath)
```

Replace prev/next button click handlers:

```tsx
onClick={() => navigate(`/articles/${prevId}`, { replace: true, state: { from: entryPath } })}
onClick={() => navigate(`/articles/${nextId}`, { replace: true, state: { from: entryPath } })}
```

Replace the keyboard shortcut path that currently calls `navigate(-1)` with `handleBack()`. Replace both 返回 button calls (`navigate(-1)`) with `handleBack`.

## Edge Cases

| Case | Behavior |
|------|----------|
| List → A → next → B → next → C → 返回 | Returns to list with prior scroll restored (existing). |
| List → A → next → B → browser back | Lands on list (replace consumed). |
| Rec card → A → 返回 | Goes to `/insights`. |
| Rec card → A: prev/next visible? | No — `articleNavList` was cleared, lookup fails, both buttons hidden by existing `prevId/nextId === null` guards. |
| Direct URL `/articles/123` (sessionStorage empty) | 返回 → `/articles`. |
| Refresh on `/articles/123` after list entry | `state` lost; `sessionStorage.articleEntryPath` survives → 返回 still works. |
| Stale `articleEntryPath` after typing direct URL in same tab | 返回 jumps to the last known entry (e.g. `/insights`). Acceptable — at worst lands on a recent meaningful page, never about:blank. |
| Reading prev/next 60 times across an >100-item list | `articleNavList` was capped at ±50 around the original click; tail of the list is unreachable via next from this entry. User can return to list and click further down. |

## Non-Goals

- No URL filter persistence change (existing sessionStorage filter restore handles that).
- No share-page changes.
- No analytics/telemetry on back-button usage.

## Testing

Manual smoke (no unit tests — pure routing behavior):

1. Open `/articles` with `unread` filter on, click 5th article. Read → next → next → 返回. Lands on list, scroll restored, filter preserved.
2. Open `/insights`, generate (or use seeded) insight, click a recommendation. Read → 返回. Lands on `/insights`, recommendation card still rendered.
3. Open new browser tab, paste `/articles/<id>` URL. Click 返回. Lands on `/articles`.
4. While on article entered from list, hit refresh. 返回 still works.
5. Verify recommendations entry hides prev/next buttons.
