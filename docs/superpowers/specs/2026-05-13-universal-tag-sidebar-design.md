# Universal Tag Sidebar — Surface Untagged Articles Across All Feeds

**Date:** 2026-05-13
**Branch:** `feature/universal-tag-sidebar`

## Problem

Tag system is already global at the data layer — `TagBar` lets users add manual tags to **any** article regardless of `feed_type`, and `article_user_tags` has no feed-type restriction. But the UI only surfaces tags on the 网摘 (`feed_type='saved'`) feed:

1. `ArticleListPage.tsx:673` hardcodes `manualTags={[]}` on every card in non-saved feeds — tags users added on regular RSS articles are invisible in lists.
2. The left tag sidebar (`SavedTagSidebar`) only mounts when `isClippingMode` is true (`ArticleListPage.tsx:531`).
3. There is **no way to find untagged articles** — the primary motivation for this work. Users want to bulk-tag content they care about, and need a "show me what I haven't tagged yet" view.

Consequence: tags feel half-finished; users have no incentive to tag regular RSS articles because the metadata disappears.

## Goal

A single collapsible tag sidebar, controlled by a top-left toggle button, that:

- Mounts on **all** feed views (regular RSS, HTML scrape, 网摘) with consistent UX
- Lets the user filter by a single tag or by "untagged"
- Combines with existing feed/unread filters as **AND**
- Shows tags on article cards in the list view
- Persists open/closed state across sessions

## Decisions

| # | Decision |
|---|----------|
| Q1 | Approach **B + collapsible**: enhance existing article list page with a tag sidebar (not a new top-level page). Default collapsed, toggled by a left-top button. |
| Q2 | Sidebar contents on non-saved feeds: **Tags only** (+「全部」/「无 tag」). Sources section stays exclusive to 网摘. |
| Q3 | Tag filter and feed dropdown combine as **AND** (intersect). Empty results show a friendly message. |
| Q4 | Tag selection is **single-select** (matches existing `SavedSelection` model). Backend already supports multi-select via `tag_ids[]` + `mode`, but YAGNI for now. |
| Q5 | 网摘 sidebar **also becomes collapsible** — global toggle controls both. Single `sidebarOpen` state, persisted to localStorage, applies regardless of feed type. |
| Q6 | Article cards in the regular list **display** `article.manual_tags` (parity with 网摘 cards). `/api/articles` returns `manual_tags` on every article. |
| Q7 | Tag counts in sidebar are **dynamic** — recomputed based on current `feed_id` / `unread` / `saved` filter. Backend endpoint accepts those params. |
| Q8 | New endpoint `GET /api/tags/sidebar` (rather than overloading `GET /api/tags`). |
| Q9 | Tag filter state stored in **sessionStorage** (matches existing pattern for `savedOnly`, `selectedFeed`, `unreadOnly`). |
| Q10 | 📚 分组 view and tag filter are **mutually exclusive**. Selecting a tag while grouped auto-disables grouping; the 📚 button is hidden when a tag filter is active. |
| -- | "Untagged" = article has **zero manual tags**. AI suggestions do not count — adopting a suggestion (which creates a manual tag) removes the article from the untagged bucket. |
| -- | Tags with `article_count = 0` under the current filter are **omitted** from the sidebar. |

## Architecture

```
ArticleListPage  (owns toggle + tagFilter state)
├── state.sidebarOpen          ← localStorage 'tagSidebarOpen' (default false)
├── state.tagFilter            ← sessionStorage 'articleTagFilter'
│                                {kind:'all'|'untagged'|'tag', id?:number}
├── SidebarToggleButton        ← top-left, before h2 "文章列表"
│
├── if !isClippingMode:
│     ├── sidebarOpen && TagSidebar (new)
│     │     全部 / 无 tag / Tags 列表(动态 count)
│     └── main area
│           ├── toolbar (existing)
│           └── ArticleCard.manualTags = article.manual_tags  ← was hardcoded []
│
└── if isClippingMode:
      └── <SavedPage restrictToFeedId={selectedFeed} sidebarOpen={sidebarOpen} />
            └── SavedPage renders SavedTagSidebar (existing, with Sources)
                when its sidebarOpen prop is true
```

**Data flow (non-saved feed, tag filter active):**

```
state change (feed_id / unread / tag_id)
  ├──→ GET /api/tags/sidebar?feed_id=X&unread=Y&saved=Z
  │     → {tags: [{id,name,article_count}], total_count, untagged_count}
  │     → renders sidebar rows
  └──→ GET /api/articles?feed_id=X&unread=Y&saved=Z&tag_id=T (or &untagged=true)
        → [Article{...,manual_tags:[UserTag]}]
        → renders main list with tag chips on cards
```

Saved mode keeps its existing `getSaved` + `SavedTagSidebar` pipeline unchanged. Only the **mount condition** of `SavedTagSidebar` changes (gated on `sidebarOpen`).

## Backend Changes

### `GET /api/articles` — extend query + response

**New query params** (mutually exclusive with each other):

| Param | Type | Behavior |
|-------|------|----------|
| `tag_id` | int | Only articles with this manual tag bound (for the current user) |
| `untagged` | bool | Only articles with **zero** manual tags (for the current user) |

If both are passed: HTTP 400.

**Response field added to each `Article`:**

```go
type Article struct {
    // ...existing fields...
    ManualTags []UserTag `json:"manual_tags"` // empty slice if none
}
```

Implementation: extend `repository/article.go::GetAll` to add WHERE clauses, then batch-load tags after the article rows are fetched (single `JOIN` query over the returned ids, then in-Go grouping by article_id — same pattern as `getSavedItems`).

**WHERE clause additions:**

```sql
-- tag_id filter
AND EXISTS (
  SELECT 1 FROM article_user_tags aut
  WHERE aut.article_id = a.id AND aut.user_id = $userID AND aut.tag_id = $tagID
)

-- untagged filter
AND NOT EXISTS (
  SELECT 1 FROM article_user_tags aut
  WHERE aut.article_id = a.id AND aut.user_id = $userID
)
```

Apply the same extension to `repository/article.go::GetGroupedByCategory` is **not** required — Q10 makes grouped + tag mutually exclusive at the frontend, so the API path with grouping never sees `tag_id`/`untagged`.

### `GET /api/tags/sidebar` — new endpoint

**Query params** (all optional):

| Param | Type | Notes |
|-------|------|-------|
| `feed_id` | int | Scope counts to a single feed |
| `unread` | bool | Scope to unread articles |
| `saved` | bool | Scope to saved articles (via `user_preferences.signal_type='save'`, same semantics as existing `getArticles(saved=true)`) |

**Response:**

```json
{
  "tags": [
    {"id": 12, "name": "AI", "article_count": 8},
    {"id": 13, "name": "Go", "article_count": 3}
  ],
  "total_count": 47,
  "untagged_count": 22
}
```

Only tags with `article_count > 0` under the filter are returned. Sorted by name ascending.

**Implementation:** new method on `UserTagRepository`, e.g. `GetTagsForSidebar(userID int, filter ArticleFilter) (SidebarData, error)`. Three queries (tag list w/ counts, total_count, untagged_count).

**Filter parity is critical**: sidebar counts must agree with what `/api/articles` returns under the same filter. Rather than duplicating the WHERE-building logic, factor the article-filter SQL out of `repository/article.go::GetAll` into a shared helper (e.g., `buildArticleFilter(filter ArticleFilter) (joinSQL, whereSQL string, args []any)`), then call it from both `GetAll` and `GetTagsForSidebar`. Saved-filter requires its `LEFT JOIN user_preferences` join; unread-filter uses `COALESCE(rp.is_completed, false) = false` against the existing reading-progress join. This is the only refactor done as part of this work and it's well-bounded.

### Unchanged

- `POST/DELETE /api/articles/{id}/tags` — tag-binding endpoints
- `GET/POST/PATCH/DELETE /api/tags` — tag CRUD
- `GET /api/saved` — saved-feed endpoint (already supports `tag_ids`/`untagged`)
- `GET /api/articles/{id}/tags` — per-article TagBar data

## Frontend Changes

### `frontend/src/api/client.ts`

```ts
// Article gets manual_tags
export interface Article {
  // ...existing...
  manual_tags: UserTag[]
}

// getArticles params extended
export const getArticles = (params?: {
  feed_id?: number
  unread?: boolean
  saved?: boolean
  tag_id?: number
  untagged?: boolean
  limit?: number
  offset?: number
}) => api.get<Article[]>('/articles', { params }).then(r => r.data)

// New sidebar endpoint
export interface TagSidebarData {
  tags: UserTag[]                    // {id, name, article_count}
  total_count: number
  untagged_count: number
}

export const getTagSidebar = (params?: {
  feed_id?: number
  unread?: boolean
  saved?: boolean
}) => api.get<TagSidebarData>('/tags/sidebar', { params }).then(r => r.data)
```

### `frontend/src/components/TagSidebar.tsx` (new)

A trimmed cousin of `SavedTagSidebar` — no Sources section.

```tsx
export type TagFilter =
  | { kind: 'all' }
  | { kind: 'untagged' }
  | { kind: 'tag'; id: number }

interface Props {
  data: TagSidebarData
  selection: TagFilter
  onSelect: (sel: TagFilter) => void
}
```

Renders:
```
[全部 (total_count)]
[(无 tag) (untagged_count)]
─── Tags ───
[tag-A]  8
[tag-B]  3
```

Single-select; clicking same row again is a no-op (use the 全部 row to reset).

### `frontend/src/components/SidebarToggleButton.tsx` (new)

Small button with a sidebar icon (◧ / ☰ / similar). Sits inside the page header, before the `<h2>文章列表</h2>`. Title: `侧栏 (T)` or similar. Clicking flips `sidebarOpen` and writes localStorage.

Keyboard shortcut: `t` (when not focused on an input). Cheap and matches existing keyboard shortcuts (`/`, `j/k`, etc.) — see `ArticleListPage.tsx:377`.

### `frontend/src/pages/ArticleListPage.tsx`

1. **New state:**

```ts
const [sidebarOpen, setSidebarOpen] = useState<boolean>(() => {
  try { return localStorage.getItem('tagSidebarOpen') === 'true' } catch { return false }
})
const [tagFilter, setTagFilter] = useState<TagFilter>(() => {
  try {
    const raw = sessionStorage.getItem('articleTagFilter')
    return raw ? JSON.parse(raw) : { kind: 'all' }
  } catch { return { kind: 'all' } }
})
const [tagSidebarData, setTagSidebarData] = useState<TagSidebarData | null>(null)
```

2. **Sidebar data fetch** (triggers on feed/unread/saved change, plus on tag-binding mutations):

```ts
useEffect(() => {
  if (!sidebarOpen || isClippingMode) return
  getTagSidebar({
    feed_id: selectedFeed || undefined,
    unread: unreadOnly || undefined,
    saved: savedOnly || undefined,
  }).then(setTagSidebarData).catch(() => setTagSidebarData(null))
}, [sidebarOpen, isClippingMode, selectedFeed, unreadOnly, savedOnly])
```

3. **loadArticles** picks up tag filter:

```ts
const raw = await getArticles({
  feed_id: selectedFeed || undefined,
  unread: unreadOnly || undefined,
  saved: savedOnly || undefined,
  tag_id: tagFilter.kind === 'tag' ? tagFilter.id : undefined,
  untagged: tagFilter.kind === 'untagged' || undefined,
  limit: PAGE_SIZE,
  offset: off,
})
```

`loadArticles` dependency array adds `tagFilter`.

4. **Card display**: `manualTags={article.manual_tags || []}` instead of `{[]}` (around line 673).

5. **Grouped exclusion** (Q10):
   - Hide the 📚 分组 button when `tagFilter.kind !== 'all'`
   - When user clicks a tag from the sidebar while `grouped === true`, auto-`setGrouped(false)`.

6. **Sidebar mount**:

```tsx
{sidebarOpen && !isClippingMode && tagSidebarData && (
  <TagSidebar
    data={tagSidebarData}
    selection={tagFilter}
    onSelect={(sel) => {
      if (grouped && sel.kind !== 'all') setGrouped(false)
      setTagFilter(sel)
      try { sessionStorage.setItem('articleTagFilter', JSON.stringify(sel)) } catch {}
    }}
  />
)}
{sidebarOpen && isClippingMode && (
  /* existing SavedPage embed already owns its own SavedTagSidebar */
)}
```

7. **Header layout** (Sidebar toggle button placement):

```tsx
<div className="page-header">
  <SidebarToggleButton open={sidebarOpen} onClick={toggle} />
  <h2>{isClippingMode ? '网摘' : '文章列表'}</h2>
  {/* ...rest of toolbar... */}
</div>
```

The outer container becomes a flex row: optional sidebar (220px) + main content. Match the existing `/saved` page layout.

### `frontend/src/pages/SavedPage.tsx`

Accept a new optional prop `sidebarOpen?: boolean` (default `true` to keep `/saved` standalone behavior unchanged). Gate the `<SavedTagSidebar>` mount on it. When embedded by `ArticleListPage` in clipping mode, the parent passes its `sidebarOpen` through; on the standalone `/saved` route it's omitted and falls back to always-on.

This avoids cross-component localStorage subscription — `ArticleListPage` is the single owner of `sidebarOpen`; the embedded `SavedPage` just receives the prop.

### `frontend/src/index.css`

Reuse `.saved-row`, `.saved-section-title` styles from the existing sidebar. Add `.tag-sidebar-toggle` button styling (small, ghost variant).

## Testing

### Backend (Go)

1. **`/api/articles?tag_id=X`** returns only articles bound to tag X for the calling user. Tag from another user is invisible.
2. **`/api/articles?untagged=true`** returns only articles with **zero** `article_user_tags` rows for the calling user (an article tagged by user A is "untagged" from user B's perspective).
3. **`/api/articles?tag_id=X&untagged=true`** → HTTP 400.
4. **`/api/articles` response shape:** every article has a `manual_tags` field (empty array if none); existing fields are unchanged.
5. **`/api/tags/sidebar`** returns only tags with `article_count > 0`. `total_count` and `untagged_count` match the result of issuing `/api/articles` with the same filter and no tag filter.
6. **Sidebar count math:** for any tag T in the result, `article_count` equals the number of articles in `/api/articles?tag_id=T` under the same scope filter.
7. **Filter combinations:** `feed_id` and `unread` and `saved` all narrow `total_count`, `untagged_count`, and per-tag counts consistently.

### Frontend (manual)

1. Toggle button flips sidebar open/closed; state persists across page reload (localStorage).
2. With sidebar open on `/articles`: clicking "无 tag" filters list to articles with no manual tags. Tag chips do not appear on those cards.
3. Click a tag row: list filters to that tag, sidebar row is highlighted. Count next to the tag matches the number of articles loaded.
4. Change `feed` dropdown while a tag is selected: list re-filters (AND), tag counts update.
5. Open 📚 分组, then click a tag in sidebar: grouping turns off automatically. Re-enable grouping: 📚 button gone while tag filter is active.
6. On 网摘 feed: sidebar shows Sources section as before (still works); same toggle button hides/shows it.
7. Add a manual tag on `/articles/:id` via `TagBar`, return to list: the tagged article shows the chip on its card.

## Out of Scope

- Multi-select tags (`tag_ids[]`, AND/OR mode) — backend already supports it for `/api/saved`; not extended to `/api/articles` here.
- Tag count on the grouped (📚) view — Q10 makes them mutually exclusive.
- Inline tag editing on the list card (must still navigate into the article).
- A standalone `/tags` top-level page — rejected (chose Approach B).

## Migration / Compat

- Existing `Article` API consumers will see a new `manual_tags` field. The shipped frontend code that doesn't read it is unaffected (the response JSON is a superset).
- `SavedPage` continues to use `/api/saved` (unchanged); only its sidebar's mount condition changes.
- No database migration. All required tables (`user_tags`, `article_user_tags`) already exist.
