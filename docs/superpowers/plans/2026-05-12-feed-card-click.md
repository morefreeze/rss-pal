# Feed Card Click → Filtered Article List Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make each subscription card on `/feeds` clickable so it navigates to `/articles` filtered to that feed.

**Architecture:** Pure frontend change in `FeedListPage.tsx`. The feed `<div className="card">` element gets a click handler that writes `selectedFeed` to `sessionStorage` and navigates to `/articles`. The three existing action buttons (refresh/pause/delete) get `e.stopPropagation()` so they don't trigger the card navigation. `ArticleListPage` already reads `sessionStorage.selectedFeed` on mount — no changes there.

**Tech Stack:** React 18, React Router 6, TypeScript, Vite. Frontend served by nginx via Docker — verification requires `docker-compose up -d --build frontend`.

---

## File Structure

- **Modify:** `frontend/src/pages/FeedListPage.tsx`
  - Add `handleCardClick(feedId)` helper inside the component
  - Add `handleCardKeyDown(e, feedId)` helper for Enter/Space keyboard activation
  - Wrap each card's existing JSX with click + keyboard handlers + `role`/`tabIndex`/cursor styling + hover background
  - Add `e.stopPropagation()` to the three action buttons (refresh, pause/resume, delete)

No new files. No backend or API changes.

---

### Task 1: Add card click handler and apply to feed cards

**Files:**
- Modify: `frontend/src/pages/FeedListPage.tsx:449-490`

- [ ] **Step 1: Add the two handlers inside the `FeedListPage` component, near the other handlers**

Insert this block immediately **after** the `handleImportOPML` function (currently ends at line 231) and **before** `const formatDate = ...` (currently line 233):

```tsx
  const handleCardClick = (feedId: number) => {
    try { sessionStorage.setItem('selectedFeed', JSON.stringify(feedId)) } catch {}
    navigate('/articles')
  }

  const handleCardKeyDown = (e: React.KeyboardEvent, feedId: number) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      handleCardClick(feedId)
    }
  }
```

- [ ] **Step 2: Wrap the feed card with click/keyboard handlers**

Replace the existing card opening tag at line 450:

```tsx
          <div key={feed.id} className="card">
```

with this version that adds the click/keyboard handlers, `role`, `tabIndex`, cursor and hover background (using inline `onMouseEnter`/`onMouseLeave` to match the rest of this file's inline-style convention):

```tsx
          <div
            key={feed.id}
            className="card"
            role="button"
            tabIndex={0}
            onClick={() => handleCardClick(feed.id)}
            onKeyDown={(e) => handleCardKeyDown(e, feed.id)}
            onMouseEnter={(e) => { (e.currentTarget as HTMLDivElement).style.background = 'var(--surface-hover)' }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLDivElement).style.background = '' }}
            style={{ cursor: 'pointer' }}
          >
```

- [ ] **Step 3: Add `e.stopPropagation()` to the three action buttons**

In the same `feeds.map` block (lines 470-486), update each button's `onClick` to stop propagation so they don't bubble up to the card.

Replace lines 470-486:

```tsx
                {feed.is_active ? (
                  <button
                    className="secondary"
                    disabled={fetchingId === feed.id}
                    onClick={() => handleFetch(feed.id)}
                  >
                    {fetchingId === feed.id ? '抓取中...' : '刷新'}
                  </button>
                ) : null}
                <button
                  className="secondary"
                  onClick={() => handleToggleActive(feed)}
                  style={!feed.is_active ? { color: '#92400e', background: '#fef9c3' } : {}}
                >
                  {feed.is_active ? '暂停' : '继续'}
                </button>
                <button className="secondary" onClick={() => handleDelete(feed.id)}>删除</button>
```

with:

```tsx
                {feed.is_active ? (
                  <button
                    className="secondary"
                    disabled={fetchingId === feed.id}
                    onClick={(e) => { e.stopPropagation(); handleFetch(feed.id) }}
                  >
                    {fetchingId === feed.id ? '抓取中...' : '刷新'}
                  </button>
                ) : null}
                <button
                  className="secondary"
                  onClick={(e) => { e.stopPropagation(); handleToggleActive(feed) }}
                  style={!feed.is_active ? { color: '#92400e', background: '#fef9c3' } : {}}
                >
                  {feed.is_active ? '暂停' : '继续'}
                </button>
                <button
                  className="secondary"
                  onClick={(e) => { e.stopPropagation(); handleDelete(feed.id) }}
                >删除</button>
```

- [ ] **Step 4: Verify TypeScript compiles**

Run:
```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors. (If there are pre-existing errors unrelated to this file, ignore them — only block on errors mentioning `FeedListPage.tsx`.)

- [ ] **Step 5: Rebuild the frontend container and verify in browser**

Per project convention (CLAUDE.md memory: `feedback_frontend_docker_rebuild.md`), the frontend is served by nginx from a pre-built bundle. Hot reload does not apply.

Run:
```bash
docker-compose up -d --build frontend
```

Then manually verify in the browser at `/feeds`:

1. Hover over a feed card → background changes to `var(--surface-hover)`, cursor becomes pointer.
2. Click anywhere on the card body (not on a button) → navigates to `/articles`, dropdown shows the clicked feed, list only shows that feed's articles.
3. Click the "刷新" button → triggers fetch only, **no** navigation.
4. Click "暂停" (or "继续") → toggles only, no navigation.
5. Click "删除" → confirm dialog appears; canceling stays on `/feeds`. (Don't actually delete a real feed; just verify the dialog appears without navigation.)
6. Tab to a card with the keyboard → press Enter → navigates same as click.
7. Click a 已暂停 card → still navigates and shows that feed's historical articles.
8. Browser console: no warnings or errors.

If any of these fail, fix the code and rebuild before continuing.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/FeedListPage.tsx
git commit -m "$(cat <<'EOF'
feat(feeds): make subscription cards clickable to filter article list

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review Notes

- **Spec coverage:**
  - 整张卡片可点 → Task 1 Step 2 ✓
  - hover 背景 + cursor pointer → Task 1 Step 2 ✓
  - 三个 action 按钮 `stopPropagation` → Task 1 Step 3 ✓
  - 键盘可达（Enter/Space） → Task 1 Step 1 (`handleCardKeyDown`) + Step 2 (`tabIndex={0}`, `onKeyDown`) ✓
  - 已暂停 feed 也可点 → no special-casing in the handler, naturally works; verified in Step 5 item 7 ✓
  - 不改 `ArticleListPage` → not touched ✓
  - 不引入 query string → handler uses `sessionStorage` + `navigate('/articles')`, no params ✓
  - Docker rebuild required → Step 5 ✓
- **Placeholder scan:** No TBD/TODO. All code is complete.
- **Type consistency:** `handleCardClick(feedId: number)` and `handleCardKeyDown(e: React.KeyboardEvent, feedId: number)` used consistently. `navigate` already imported on line 2. `sessionStorage.selectedFeed` key matches what `ArticleListPage.tsx:151` reads.
