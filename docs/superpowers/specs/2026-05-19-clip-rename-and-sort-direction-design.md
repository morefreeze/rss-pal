# Clip rename + in-page tab + sort direction

Date: 2026-05-19

## Motivation

Two paper-cuts on `/articles`:

1. **Tab jumps away.** On `/articles`, tapping the 网摘 bottom tab navigates to `/saved`, a separate page. But `ArticleListPage` already implements an in-place "clipping mode" — when the user picks the `feed_type='saved'` pseudo-feed in the dropdown, the article list swaps in the tag-sidebar layout that used to live at `/saved`. The mobile tab still points at the old standalone route, so the user gets ejected from `/articles` instead of switching the view inside it.
2. **Sort is one-dimensional.** The sort toggle on `/articles` flips between 发布时间 and 抓取时间, both forced to DESC. There's no way to view oldest-first for either field.

Bundled with these: a naming cleanup. The "网摘 bin" pseudo-feed and its surrounding code use `saved` in English, which collides with the orthogonal `signal_type='save'` star/bookmark signal. We rename the 网摘 chain to `clip` and leave the star signal alone.

## Scope

In:
- Mobile/bottom-nav tab navigates to `/articles?view=clip` instead of `/saved`.
- `ArticleListPage` reads `?view=clip` and selects the clip pseudo-feed; manual dropdown changes update the URL back.
- Sort UI on `/articles` becomes two narrow buttons (`发布时间` / `抓取时间`), each with a direction arrow; clicking the active one toggles asc/desc.
- Backend `/api/articles` accepts `order=asc|desc` in addition to existing `sort=published|captured`.
- Rename `feed_type='saved'` → `'clip'` across DB, backend, and frontend (route, page, component, types, comments).
- **Behavior change**: `/api/clip` (formerly `/api/saved`) returns ONLY articles from the clip pseudo-feed — the star-signal OR branch is removed. Star-saved articles are reached via the existing 已保存 checkbox on `/articles`.

Out:
- `signal_type='save'`, the `savedOnly` filter, the 已保存 checkbox label, and `up_save` SQL aliases stay as-is (different semantic — per-article star, not 网摘 bin).
- Backup snapshot field names (`SavedArticles`, `saved_articles`) stay — they mean "rows that were saved by pg_dump", unrelated to 网摘.
- Chinese UI label stays "网摘".
- No new sort/order on `/api/clip`, `getGroupedArticles`, search, recommended.

## Design

### Part 1 — In-page tab switch via `?view=clip`

**`components/MobileTabBar.tsx`**
- 网摘 entry's `to` becomes `/articles?view=clip`. 文章 entry stays `/articles`.
- Drop `NavLink`'s built-in `isActive`. Read `useLocation()` and compute:
  - `articlesActive = pathname === '/articles' && new URLSearchParams(search).get('view') !== 'clip'`
  - `clipActive    = pathname === '/articles' && new URLSearchParams(search).get('view') === 'clip'`
- Pass the booleans to the existing `tabStyle(active)` helper.

**`pages/ArticleListPage.tsx`**
- Use `useSearchParams`. Define `wantsClip = searchParams.get('view') === 'clip'`.
- After feeds load, reconcile selection:
  - If `wantsClip` and the currently-selected feed isn't the clip pseudo-feed: find `feeds.find(f => f.feed_type === 'clip')`, set `selectedFeed = clipFeed.id`, mirror to sessionStorage.
  - If `wantsClip` but no clip feed exists yet (rare — user hasn't installed the extension): set `selectedFeed = null` and show an inline empty hint (e.g. "还没有网摘，安装扩展或书签后再来") above the (empty) list. Don't try to render the clip layout without a clip feed.
  - If `!wantsClip` and the selected feed *is* the clip pseudo-feed: clear back to `null` (or to the last non-clip selection if we tracked it — see below).
- When the dropdown changes selection manually, sync the URL:
  - Picked clip feed: `setSearchParams({ view: 'clip' })`.
  - Picked anything else: `setSearchParams({})` (drops `view`).
- The "last non-clip selection" memory is not required for v1 — clearing to "all feeds" is fine when switching back to 文章. If we want it later, store under `lastNonClipFeed` in sessionStorage.
- `isClippingMode` derivation updates: `feed_type === 'clip'`.

**Routes (`App.tsx`)**
- Replace `<Route path="saved" element={<SavedPage />} />` with `<Route path="clip" element={<ClipPage />} />`.
- The catch-all already redirects to `/articles`, so deleted `/saved` URLs degrade gracefully.

### Part 2 — Sort field × direction

**State**

```ts
type SortField = 'published' | 'captured'
type SortDir   = 'asc' | 'desc'
```

- `sortField` persisted in sessionStorage key `articlesSortField` (default `'published'`).
- `sortDir` persisted in sessionStorage key `articlesSortDir` (default `'desc'`).
- Migration: if `articlesSortField` is missing but the old `articlesSort` exists, seed `sortField` from it (`'captured' → 'captured'`, else `'published'`). Old key can be cleared.

**UI**

Replace the single button in `ArticleListPage.tsx` (currently around line 558–571) with two siblings:

```
[发布时间 ↓] [抓取时间]
```

Narrow buttons; active one styled with the existing accent treatment, inactive uses `btn-ghost`. Each button's label is `<field> <arrow>` where arrow comes only from the *active* button (the inactive button shows just the label, to keep them visually quiet). The `title` attribute spells out the action ("点击切换为 X" / "再点切换升序").

Click handler:
```ts
const onPick = (field: SortField) => {
  if (field === sortField) {
    setSortDir(d => d === 'desc' ? 'asc' : 'desc')
  } else {
    setSortField(field) // keeps current sortDir
  }
}
```

Both keys mirror to sessionStorage on change. Hidden conditions unchanged: still only visible when `!isClippingMode && !searchQuery && !grouped`.

**API client (`api/client.ts`)**
- `ArticleSort` stays `'published' | 'captured'` (the field).
- Add `ArticleOrder = 'asc' | 'desc'` and `order?: ArticleOrder` to `getArticles` params; URL: `&order=asc` when present.

**Backend**

- `internal/repository/article.go`:
  - Add `type SortDir int` with `SortDesc = iota; SortAsc`.
  - `GetAll(..., sort SortMode, dir SortDir)`. Use a helper `dirSQL(dir SortDir) string` returning `"DESC"` or `"ASC"`.
  - SortCaptured branch: `ORDER BY articles.fetched_at <DIR>`.
  - SortPublished branch: both expressions take the same direction:
    ```
    ORDER BY DATE_TRUNC('day', GREATEST(COALESCE(articles.published_at, articles.fetched_at), articles.fetched_at - INTERVAL '7 days')) <DIR>,
             COALESCE(articles.published_at, articles.fetched_at) <DIR>
    ```
- `internal/api/article.go`: parse `order` (default `desc`), pass to `GetAll`. Handler currently around `articles, err := h.articleRepo.GetAll(...)`.
- Other callers of `GetAll` (grep) — pass `SortDesc` so behavior doesn't change.
- Tests: if there is an `article_test.go` covering GetAll, add an ASC case for each field. If no test file exists for GetAll today, leave testing to manual verification (matches repo's current testing posture).

### Part 3 — `saved` → `clip` rename

**DB migration**

`backend/migrations/024_rename_feed_type_saved_to_clip.sql`:

```sql
-- Rename the 网摘 pseudo-feed type. The "saved" name collided with
-- user_preferences.signal_type='save' (star/bookmark), which is unrelated.
UPDATE feeds SET feed_type = 'clip' WHERE feed_type = 'saved';
```

Per CLAUDE.md: this file is auto-applied only on a fresh volume. On the existing dev DB, the operator runs `psql -U postgres -d rsspal -f backend/migrations/024_rename_feed_type_saved_to_clip.sql` manually after deploy. The implementation plan must call this out as a non-skippable step.

DB-touching action — needs the standard safety dance (backup taken before applying, restore steps documented). See `internal/backup` for the existing snapshot tooling.

**Backend file moves & renames**

- `internal/api/saved.go` → `internal/api/clip.go`. Inside: `SavedHandler` → `ClipHandler`, `NewSavedHandler` → `NewClipHandler`, the `GET /api/saved` comment becomes `GET /api/clip`.
- The repository types currently live in `internal/repository/user_tag.go` (`SavedRepository`, `SavedQuery`, `SavedRow`, `EffectiveSource`, `ListSaved`). Move them to a new file `internal/repository/clip.go` and rename:
  - `SavedRepository` → `ClipRepository`, `NewSavedRepository` → `NewClipRepository`
  - `SavedQuery` → `ClipQuery`, `SavedRow` → `ClipRow`
  - `ListSaved` → `ListClipped`
  - `EffectiveSource` keeps its name (it's reused for /clip rows and is generic-enough).
- `ListClipped` SQL: drop the `signal_type='save'` branch from the top-level WHERE.
  - Before:
    ```sql
    where := []string{`(
      EXISTS (SELECT 1 FROM user_preferences p
              WHERE p.article_id = a.id AND p.user_id = $1 AND p.signal_type = 'save')
      OR (f.feed_type = 'saved' AND f.owner_id = $1)
    )`}
    ```
  - After:
    ```sql
    where := []string{`f.feed_type = 'clip' AND f.owner_id = $1`}
    ```
  - Tenancy guard `(f.owner_id IS NULL OR f.owner_id = $1)` stays for safety, though it's now redundant with the above. Keep the redundancy — it's cheap and matches the pattern used elsewhere.
- `cmd/server/main.go`:
  - Variable `savedHandler` → `clipHandler`, `savedRepo` → `clipRepo`.
  - Route `apiGroup.GET("/saved", ...)` → `apiGroup.GET("/clip", ...)`.
- String literal updates anywhere `feed_type = 'saved'`, `feed_type IN ('link_set', 'saved')`, `'saved'` appears for this purpose:
  - `internal/repository/article.go` (line ~1499, plus `GetEffectiveSource`-style helpers around 332)
  - Worker code that scans clip-eligible feeds (grep first for callers)
  - `cmd/seed/...` if it inserts a clip feed
  - Backup snapshot code: `internal/backup/snapshot_saved.go` — **only** for feed_type predicates inside SQL. Do NOT rename the file or the `SavedArticles` struct/JSON field — those refer to "rows saved into the snapshot", which is a different meaning.
- Comments mentioning "saved-bin", "clipping mode", "网摘 (clipping)" updated to "clip".

**Frontend file moves & renames**

- `pages/SavedPage.tsx` → `pages/ClipPage.tsx`. Default export `SavedPage` → `ClipPage`. Inside:
  - Imports of `getSaved`, `GetSavedParams`, `SavedItem`, `SavedListResponse` from `../api/client` → renamed.
  - Inline comment/JSDoc that says "embedded inside ArticleListPage as the 网摘 (clipping) feed view" → unchanged narrative, but "clipping" → "clip".
  - `entryPath` default stays `'/saved'`? Change to `'/clip'`.
- `components/SavedTagSidebar.tsx` → `ClipTagSidebar.tsx`. Exported types `SavedSelection` → `ClipSelection`, `SavedSourceRow` → `ClipSourceRow`.
- `components/SavedTagChipBar.tsx` → `ClipTagChipBar.tsx`.
- CSS classes `saved-row`, `saved-row-label`, `saved-section-title` → `clip-row`, `clip-row-label`, `clip-section-title`. Update the CSS file (likely `index.css` — verify).
- `api/client.ts`:
  - `getSaved` → `getClip`; request path string `/saved` → `/clip`.
  - Type renames: `GetSavedParams` → `GetClipParams`, `SavedItem` → `ClipItem`, `SavedListResponse` → `ClipListResponse`.
- `App.tsx`: route swap.
- `ArticleListPage.tsx`:
  - `isClippingMode` derivation: `feed_type === 'clip'`.
  - Imports for `SavedPage` → `ClipPage`.
  - The 网摘 dropdown label / `<h2>{isClippingMode ? '网摘' : '文章列表'}` stays — that's the Chinese UI label, not affected.

**Explicit non-renames (recap)**
- `user_preferences.signal_type='save'` (DB enum value): unchanged.
- `savedOnly` Go variable + `?saved=true` query param on `/api/articles`, `/api/articles/grouped`, `/api/articles/mark-all-read`: unchanged.
- 已保存 checkbox label and `savedOnly` React state: unchanged.
- `up_save` SQL alias: unchanged.
- `internal/backup/snapshot_saved.go`, `SavedArticles`/`saved_articles` struct/JSON keys: unchanged.

## Out of scope / explicitly deferred

- Adding sort/order to `/api/clip`, search, grouped, recommended. The current UI does not expose sort in those modes; revisit if asked.
- Tracking "last non-clip feed" so the 文章 tab restores prior selection. Worth doing if users complain, not before.
- Cleaning up the legacy `articlesSort` sessionStorage key after a few releases — the migration just ignores it; old keys age out on their own.

## Risks

- **Migration ordering**: the rename migration `024` updates rows. If the implementation lands but the migration isn't applied to the live DB, the backend code that searches for `feed_type='clip'` won't find anything and the 网摘 view goes empty. Mitigation: implementation plan opens with the migration step + verification (`SELECT COUNT(*) FROM feeds WHERE feed_type='saved';` returns 0 after apply).
- **Frontend/backend lag**: if frontend ships expecting `/api/clip` before backend deploys, `/articles?view=clip` will 404 on the API call. Mitigation: keep PR scoped to a single deploy unit (no need for compat shim given this is a single-operator deploy).
- **Hard-coded literals missed**: grep for the string `'saved'` and `"saved"` after the rename and check every hit. Anything still in the codebase under a feed_type context is a bug.

## Verification

1. Migration applied, `SELECT feed_type, COUNT(*) FROM feeds GROUP BY feed_type;` shows `clip` rows where `saved` used to be, and zero `saved` rows.
2. From `/articles`, tap 网摘 in the bottom tab — URL becomes `/articles?view=clip`, list switches to the clip view, dropdown reads the clip pseudo-feed.
3. From clip view, tap 文章 in the bottom tab — URL returns to `/articles`, dropdown clears.
4. From clip view, manually pick a different feed in the dropdown — URL drops `?view=clip`.
5. Sort buttons: 发布时间 ↓ active. Tap 发布时间 → 发布时间 ↑, oldest-first list. Tap 抓取时间 → keeps ↑ direction. Tap 抓取时间 again → ↓. Reload page → state restored from sessionStorage.
6. `/api/clip` returns only feed_type='clip' items; star-saved RSS articles do not appear there.
7. Old `/saved` URL 302s (catch-all redirects to `/articles`). Acceptable per user direction.
