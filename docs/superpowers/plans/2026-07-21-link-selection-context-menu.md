# Link Selection Context Menu Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace explicit link_set conversion and per-link mailbox icons with a reusable selection-driven reader menu, persistent ordered link drafts, and atomic first-batch promotion while fixing stale article-detail ETags.

**Architecture:** A generic reader interaction layer converts desktop Selections and mobile long presses into immutable link targets, then asks a link_set adapter for actions. ArticlePage owns versioned per-article drafts and confirmation, while the backend validates arbitrary HTTP(S) links and promotes the parent in the same transaction that inserts children. Detail ETags hash the exact serialized response so every visible state participates in validation.

**Tech Stack:** React 18, TypeScript, React Markdown, Vitest, Testing Library, jsdom, Go 1.24, Gin, PostgreSQL repositories with RLS request transactions.

---

## File map

- Test foundation: modify `frontend/package.json`, lockfile, and existing TS tests; create `frontend/vitest.config.ts` and `frontend/test/setup.ts`.
- Reader core: create `frontend/src/reader/{types,selectionLinks,ReaderActionContext,ReaderContextMenu,ReaderInteractionSurface,linkSetActions}.*` plus focused tests.
- Draft/UI integration: modify `linkSetSelection.ts`, `url.ts`, `MarkdownArticle.tsx`, `BatchFetchConfirmDialog.tsx`, `ReadingLayout.tsx`, `ArticlePage.tsx`, and `index.css`; delete `LinkSetMarkIcon.tsx` and `LinkSetContext.tsx` after migration.
- Backend: modify RSS URL normalization, create API batch validation, add atomic repository promotion, and compute detail ETags from exact JSON bytes.

## Task 1: Establish a frontend DOM test command

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/package-lock.json`
- Create: `frontend/vitest.config.ts`
- Create: `frontend/test/setup.ts`
- Modify: `frontend/test/readingProgress.test.ts`
- Modify: `frontend/test/popularFeedsVisibility.test.ts`

- [ ] **Step 1: Install test dependencies**

```bash
cd frontend
npm install --save-dev vitest jsdom @testing-library/react @testing-library/user-event
```

Expected: exit 0; only package metadata changes.

- [ ] **Step 2: Add configuration**

Add these scripts: `"test": "vitest run"`, `"test:legacy": "node --test test/*.test.cjs"`, and `"check": "npm test && npm run test:legacy"`. Create `vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: { environment: 'jsdom', setupFiles: ['./test/setup.ts'], restoreMocks: true },
})
```

Create `test/setup.ts`:

```ts
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'
afterEach(() => cleanup())
```

- [ ] **Step 3: Convert existing TS checks into named Vitest cases**

Import `describe`, `expect`, and `it`; preserve every input and expected value. Replace hand-written helpers with `toBe` and `toBeCloseTo`.

- [ ] **Step 4: Verify and commit**

```bash
npm run check
npm run build
git add package.json package-lock.json vitest.config.ts test/setup.ts test/readingProgress.test.ts test/popularFeedsVisibility.test.ts
git commit -m "test: add frontend DOM test harness"
```

Expected: converted TS cases, 3 CJS files, and production build all pass.

## Task 2: Share URL normalization and validate arbitrary batches

**Files:**
- Modify: `backend/internal/rss/linkset_extract.go`
- Modify: `backend/internal/rss/linkset_extract_test.go`
- Create: `backend/internal/api/link_set_request.go`
- Create: `backend/internal/api/link_set_request_test.go`
- Modify: `backend/internal/api/article.go`

- [ ] **Step 1: Write failing tests**

Add RSS coverage:

```go
func TestNormalizeLinkSetURL(t *testing.T) {
    got, err := NormalizeLinkSetURL("https://Example.COM/a/?utm_source=x&b=2&a=1#part")
    if err != nil { t.Fatal(err) }
    if got != "https://example.com/a?a=1&b=2" { t.Fatalf("got %q", got) }
    for _, raw := range []string{"mailto:a@b.com", "javascript:void(0)", "/relative"} {
        if _, err := NormalizeLinkSetURL(raw); err == nil { t.Fatalf("expected %q to fail", raw) }
    }
}
```

Create API tests asserting trim/normalize/dedupe order, empty input, 101 entries, non-HTTP schemes, missing host, URL >4096 runes, title >500, and editor note >2000 all reject the complete batch.

- [ ] **Step 2: Verify RED**

```bash
cd backend
go test ./internal/rss ./internal/api -run 'NormalizeLinkSetURL|ValidateBatchFetchCandidates' -count=1
```

Expected: compile failure for missing normalizer/validator types.

- [ ] **Step 3: Export the canonical normalizer**

Keep `normaliseLinkSetURL(*url.URL)` and add:

```go
func NormalizeLinkSetURL(raw string) (string, error) {
    u, err := url.ParseRequestURI(raw)
    if err != nil { return "", fmt.Errorf("invalid URL: %w", err) }
    if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
        return "", fmt.Errorf("URL must be absolute http(s)")
    }
    return normaliseLinkSetURL(u), nil
}
```

- [ ] **Step 4: Implement named DTOs and validation**

Create `link_set_request.go` with:

```go
const (
    maxBatchFetchCandidates = 100
    maxBatchURLRunes = 4096
    maxBatchTitleRunes = 500
    maxBatchEditorNoteRunes = 2000
)
type BatchFetchCandidate struct { Title string `json:"title"`; URL string `json:"url"`; EditorNote string `json:"editor_note"` }
type BatchFetchRequest struct { Candidates []BatchFetchCandidate `json:"candidates"` }
```

`validateBatchFetchCandidates` uses `utf8.RuneCountInString`, trims title/note, normalizes with `rss.NormalizeLinkSetURL`, dedupes normalized URLs preserving first order, and falls back from empty title to URL. Delete the anonymous DTO from `article.go`.

- [ ] **Step 5: Verify GREEN and commit**

```bash
gofmt -w internal/rss/linkset_extract.go internal/rss/linkset_extract_test.go internal/api/link_set_request.go internal/api/link_set_request_test.go internal/api/article.go
go test ./internal/rss ./internal/api -run 'NormalizeLinkSetURL|ValidateBatchFetchCandidates' -count=1
git add internal/rss/linkset_extract.go internal/rss/linkset_extract_test.go internal/api/link_set_request.go internal/api/link_set_request_test.go internal/api/article.go
git commit -m "feat: validate arbitrary link set batches"
```

## Task 3: Make promotion and child insertion atomic

**Files:**
- Modify: `backend/internal/repository/link_set.go`
- Create: `backend/internal/repository/link_set_atomic_test.go`

- [ ] **Step 1: Write failing transaction tests**

Seed one feed and parent with `testdb.New(t)`. Call the missing method once with an invalid feed ID and assert insertion errors while `links_extendable` remains SQL NULL; call it with the valid feed and assert one child plus `links_extendable=true`.

```go
_, err := repo.EnableAndInsertLinkSetChildren(parentID, []LinkSetChildInput{{
    FeedID: feedID + 999999, ParentArticleID: parentID,
    Title: "bad", URL: "https://child/bad", ProcessingState: "processing",
}})
if err == nil { t.Fatal("expected foreign-key failure") }
```

- [ ] **Step 2: Verify RED**

```bash
go test ./internal/repository -run EnableAndInsertLinkSetChildren -count=1
```

Expected: compile failure for the missing method.

- [ ] **Step 3: Extract insertion on an existing Querier**

Move the existing SQL/row scan into `insertLinkSetChildren(q Querier, children []LinkSetChildInput) (int, error)`. Keep `InsertLinkSetChildren` wrapping it with `txOrBegin`, rollback defer, and commit.

- [ ] **Step 4: Implement atomic enable-and-insert**

```go
func (r *ArticleRepository) EnableAndInsertLinkSetChildren(parentID int, children []LinkSetChildInput) (int, error) {
    tx, commit, rollback, err := txOrBegin(r.db)
    if err != nil { return 0, err }
    defer rollback()
    result, err := tx.Exec(`UPDATE articles SET links_extendable=true WHERE id=$1`, parentID)
    if err != nil { return 0, err }
    affected, err := result.RowsAffected()
    if err != nil || affected != 1 { return 0, fmt.Errorf("parent article %d not found", parentID) }
    inserted, err := insertLinkSetChildren(tx, children)
    if err != nil { return 0, err }
    if err := commit(); err != nil { return 0, err }
    return inserted, nil
}
```

- [ ] **Step 5: Verify GREEN and commit**

```bash
gofmt -w internal/repository/link_set.go internal/repository/link_set_atomic_test.go
go test ./internal/repository -run 'EnableAndInsertLinkSetChildren|InsertLinkSetChildren' -count=1
go test ./internal/repository -count=1
git add internal/repository/link_set.go internal/repository/link_set_atomic_test.go
git commit -m "feat: atomically enable link sets on batch insert"
```

## Task 4: Wire validated requests into BatchFetch

**Files:**
- Modify: `backend/internal/api/article.go`
- Modify: `backend/internal/api/link_set_request_test.go`

- [ ] **Step 1: Write a failing mapper test**

Test `linkSetChildInputs(parent, candidates)` copies parent/feed IDs, publication time, normalized fields, score 0, and `processing` state.

```go
published := time.Unix(123, 0)
parent := &model.Article{ID: 9, FeedID: 4, PublishedAt: &published}
got := linkSetChildInputs(parent, []BatchFetchCandidate{{Title: "A", URL: "https://example.com/a"}})
if len(got) != 1 || got[0].ParentArticleID != 9 || got[0].ProcessingState != "processing" { t.Fatalf("%+v", got) }
```

- [ ] **Step 2: Verify RED**

```bash
go test ./internal/api -run LinkSetChildInputsUseParentMetadata -count=1
```

Expected: compile failure for the missing mapper.

- [ ] **Step 3: Implement handler mapping**

After `ShouldBindJSON`:

```go
candidates, err := validateBatchFetchCandidates(req.Candidates)
if err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return }
inputs := linkSetChildInputs(parent, candidates)
n, err := articleRepo.EnableAndInsertLinkSetChildren(parent.ID, inputs)
```

Remove silent empty-URL skipping and the direct `InsertLinkSetChildren` call.

- [ ] **Step 4: Verify and commit**

```bash
gofmt -w internal/api/article.go internal/api/link_set_request_test.go
go test ./internal/api -run 'ValidateBatchFetchCandidates|LinkSetChildInputs' -count=1
go test ./... -count=1
git add internal/api/article.go internal/api/link_set_request_test.go
git commit -m "feat: promote link sets on first batch fetch"
```

## Task 5: Compute detail ETags from exact response bytes

**Files:**
- Modify: `backend/internal/api/etag.go`
- Modify: `backend/internal/api/etag_test.go`
- Modify: `backend/internal/api/article.go`

- [ ] **Step 1: Write failing representation tests**

Retain list ETag tests. Replace article-only detail tests with `MarshalDetailResponse` stability plus table cases changing `links_extendable`, children processing state, progress, signals, and hidden. Each must change the tag; identical `gin.H` input must produce identical bytes/tag and `W/"..."` format.

```go
body1, tag1, err := api.MarshalDetailResponse(base)
if err != nil { t.Fatal(err) }
body2, tag2, err := api.MarshalDetailResponse(base)
if err != nil || string(body1) != string(body2) || tag1 != tag2 { t.Fatal("unstable detail response") }
```

- [ ] **Step 2: Verify RED**

```bash
go test ./internal/api -run MarshalDetailResponse -count=1
```

Expected: compile failure for missing `MarshalDetailResponse`.

- [ ] **Step 3: Implement exact serialization**

```go
func MarshalDetailResponse(response any) ([]byte, string, error) {
    body, err := json.Marshal(response)
    if err != nil { return nil, "", err }
    h := sha256.New()
    h.Write([]byte("detail-v2|"))
    h.Write(body)
    return body, `W/"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`, nil
}
```

- [ ] **Step 4: Reorder GetByID**

Load progress/signals/hidden/children first, build `response`, call `MarshalDetailResponse`, set private no-cache and ETag, return 304 only for the full tag, otherwise send the same bytes via `c.Data(..., "application/json; charset=utf-8", body)`. Remove `ComputeDetailETag(model.Article)` after its last caller migrates.

- [ ] **Step 5: Verify and commit**

```bash
gofmt -w internal/api/etag.go internal/api/etag_test.go internal/api/article.go
go test ./internal/api -run ETag -count=1
go test ./... -count=1
git add internal/api/etag.go internal/api/etag_test.go internal/api/article.go
git commit -m "fix: validate article detail cache against full response"
```

## Task 6: Upgrade link drafts to ordered versioned records

**Files:**
- Modify: `frontend/src/utils/linkSetSelection.ts`
- Modify: `frontend/src/utils/url.ts`
- Create: `frontend/test/linkSetSelection.test.ts`

- [ ] **Step 1: Write failing storage tests**

Cover v2 round trip, v1 migration, TTL expiry, corrupt JSON, Storage failures, invalid schemes, dedupe, and title enrichment.

```ts
it('migrates v1 URL arrays into ordered drafts', () => {
  localStorage.setItem('rsspal_batch_sel_7', JSON.stringify({
    urls: ['https://Example.com/a/?utm_source=x'], savedAt: 1_000,
  }))
  expect(loadDraftLinks(7, localStorage, 2_000)).toEqual([{
    url: 'https://example.com/a', title: 'example.com', addedAt: 1_000,
  }])
})
```

Assert `normalizeHTTPURL('mailto:x@y', base)` is null and a relative link resolves against the article URL.

- [ ] **Step 2: Verify RED**

```bash
cd frontend
npm test -- test/linkSetSelection.test.ts
```

Expected: compile failure for missing draft helpers.

- [ ] **Step 3: Implement v2 helpers**

Export:

```ts
export type DraftLink = { url: string; title: string; addedAt: number }
export function loadDraftLinks(articleId: number, storage: Storage = localStorage, now = Date.now()): DraftLink[]
export function saveDraftLinks(articleId: number, links: DraftLink[], storage: Storage = localStorage, now = Date.now()): void
export function enrichDraftLinkTitle(link: DraftLink, discoveredTitle: string): DraftLink
```

Use `{ version: 2, links, savedAt }`, retain the 24-hour TTL, normalize/dedupe both versions preserving first order, remove the key when empty, and catch all Storage/JSON errors. Add `normalizeHTTPURL(href, base): string | null` to `url.ts`, delegating canonicalization to `normalizeURL` then rejecting non-HTTP schemes.

- [ ] **Step 4: Verify and commit**

```bash
npm test -- test/linkSetSelection.test.ts
npm run build
git add src/utils/linkSetSelection.ts src/utils/url.ts test/linkSetSelection.test.ts
git commit -m "feat: persist ordered link fetch drafts"
```

## Task 7: Resolve partial Selections to complete link targets

**Files:**
- Create: `frontend/src/reader/types.ts`
- Create: `frontend/src/reader/selectionLinks.ts`
- Create: `frontend/test/selectionLinks.test.ts`

- [ ] **Step 1: Write failing DOM Range tests**

The primary case selects only characters 3..7 inside an anchor and expects the complete anchor URL/title/element:

```ts
const text = root.querySelector('a')!.firstChild!
const range = document.createRange()
range.setStart(text, 3)
range.setEnd(text, 7)
const selection = window.getSelection()!
selection.removeAllRanges()
selection.addRange(range)
const targets = selectionLinkTargets(root, selection, normalize)
expect(targets[0].title).toBe('Readable Link')
expect(targets[0].element).toBe(root.querySelector('a'))
```

Add cases entering/leaving a link, two links, zero-width boundary contact, plain text, code links, outside-root Selection, invalid schemes, and duplicate normalized URLs retaining the first anchor.

- [ ] **Step 2: Verify RED**

```bash
npm test -- test/selectionLinks.test.ts
```

Expected: module-not-found failure.

- [ ] **Step 3: Define generic types**

`types.ts` defines `ReaderLinkTarget`, `ReaderContextTarget` (`selection-links` or `long-press-link`), `ReaderContextAction`, and:

```ts
export type ReaderActionContextValue = {
  normalizeLink(href: string): string | null
  getLinkState(url: string): 'draft' | 'fetched' | null
  getActions(target: ReaderContextTarget): ReaderContextAction[]
  onLinkDiscovered?(link: Pick<ReaderLinkTarget, 'url' | 'title'>): void
}
```

- [ ] **Step 4: Implement non-empty overlap extraction**

Reject collapsed/outside Selections, iterate anchors in DOM order, skip `pre a, code a`, intersect the Selection Range with each anchor-content Range, and require non-empty intersection text. Normalize/filter URLs, collapse whitespace in the full anchor title, dedupe by URL, and return the actual anchor. Export `linkTargetFromAnchor` for mobile reuse.

- [ ] **Step 5: Verify and commit**

```bash
npm test -- test/selectionLinks.test.ts
git add src/reader/types.ts src/reader/selectionLinks.ts test/selectionLinks.test.ts
git commit -m "feat: resolve selected text to complete link targets"
```

## Task 8: Build the generic accessible menu

**Files:**
- Create: `frontend/src/reader/ReaderActionContext.tsx`
- Create: `frontend/src/reader/ReaderContextMenu.tsx`
- Create: `frontend/test/ReaderContextMenu.test.tsx`

- [ ] **Step 1: Write failing menu tests**

Test portal rendering, `menu/menuitem` roles, first-action focus, arrow navigation, Enter/Space execution, Escape/outside/Tab close, busy disabling, rejection staying open, and viewport clamping.

```tsx
render(<ReaderContextMenu open anchorRect={new DOMRect(20, 30, 40, 10)}
  actions={[{ id: 'add', label: '加入待抓取（1）', run }]}
  onClose={onClose} />)
const item = await screen.findByRole('menuitem', { name: '加入待抓取（1）' })
expect(document.activeElement).toBe(item)
fireEvent.keyDown(item, { key: 'Enter' })
expect(run).toHaveBeenCalledTimes(1)
```

- [ ] **Step 2: Verify RED**

```bash
npm test -- test/ReaderContextMenu.test.tsx
```

Expected: module-not-found failure.

- [ ] **Step 3: Implement provider and menu**

`ReaderActionContext.tsx` exports a nullable context. `ReaderContextMenu` uses `createPortal` to `document.body`, fixed positioning with 8px margins, roving focus, and `busyActionID`. Menu `pointerdown` calls `preventDefault()` so browser focus changes do not destroy the saved Selection. Await actions; close on success, retain on rejection.

- [ ] **Step 4: Verify and commit**

```bash
npm test -- test/ReaderContextMenu.test.tsx
git add src/reader/ReaderActionContext.tsx src/reader/ReaderContextMenu.tsx test/ReaderContextMenu.test.tsx
git commit -m "feat: add reusable reader context menu"
```

## Task 9: Implement Selection and long-press lifecycles

**Files:**
- Create: `frontend/src/reader/ReaderInteractionSurface.tsx`
- Create: `frontend/test/ReaderInteractionSurface.test.tsx`

- [ ] **Step 1: Write failing desktop tests**

Cover partial Selection plus pointerup opening the menu, full-anchor `.reader-context-target`, pure text staying closed, multiple targets, mouse `contextmenu.defaultPrevented === false`, drag-selection click suppression, ordinary click, Escape, scroll, Selection replacement, and unmount cleanup.

- [ ] **Step 2: Write failing touch tests**

With fake timers and `pointerType:'touch'`, assert no menu at 499ms, menu at 500ms, cancellation beyond 8px, scroll/pointercancel/early-release cancellation, next-click suppression, and suppression of only the touch-generated native contextmenu.

- [ ] **Step 3: Verify RED**

```bash
npm test -- test/ReaderInteractionSurface.test.tsx
```

Expected: module-not-found failure.

- [ ] **Step 4: Implement immutable target snapshots**

The surface holds `rootRef`, resolves actions from `ReaderActionContext`, and renders `ReaderContextMenu`. Opening copies links/rect into state and adds `.reader-context-target` to entire anchors. Keep that snapshot if menu focus collapses Selection; remove classes only on close, actual target replacement, article switch, or unmount.

- [ ] **Step 5: Implement scoped long press**

Within the surface, delegate pointer events to contained anchors. Track start coordinates and a 500ms timer. Cancel at >8px or scroll/cancel/release. Use one-shot refs for the following click and touch contextmenu; never prevent a mouse contextmenu.

- [ ] **Step 6: Verify and commit**

```bash
npm test -- test/ReaderInteractionSurface.test.tsx
git add src/reader/ReaderInteractionSurface.tsx test/ReaderInteractionSurface.test.tsx
git commit -m "feat: open reader actions from selection and long press"
```

## Task 10: Generate link_set actions independently

**Files:**
- Create: `frontend/src/reader/linkSetActions.ts`
- Create: `frontend/test/linkSetActions.test.ts`

- [ ] **Step 1: Write failing action matrix tests**

Test unmarked, marked, mixed, fetched, and long-press targets. Assert exact labels, target order, callback URLs, and that long press includes open/copy while desktop Selection does not.

```ts
expect(actions.map(a => a.label)).toEqual(['加入待抓取（1）', '移出待抓取（1）'])
await actions[0].run()
expect(add).toHaveBeenCalledWith([expect.objectContaining({ url: 'https://a' })])
```

- [ ] **Step 2: Verify RED**

```bash
npm test -- test/linkSetActions.test.ts
```

Expected: module-not-found failure.

- [ ] **Step 3: Implement the adapter**

Accept target, draft/fetched sets, and `onAdd/onRemove/onOpen/onCopy` callbacks. Preserve target order. Selection gets add/remove/readonly fetched actions; long press additionally gets `在新标签页打开` and `复制链接`.

- [ ] **Step 4: Verify and commit**

```bash
npm test -- test/linkSetActions.test.ts
git add src/reader/linkSetActions.ts test/linkSetActions.test.ts
git commit -m "feat: map reader targets to link set actions"
```

## Task 11: Render reader actions and link state in both modes

**Files:**
- Modify: `frontend/src/components/MarkdownArticle.tsx`
- Modify: `frontend/src/components/ReadingLayout.tsx`
- Modify: `frontend/src/index.css`
- Create: `frontend/test/MarkdownArticleLinkActions.test.tsx`
- Create: `frontend/test/ReadingLayoutLinkActions.test.tsx`

- [ ] **Step 1: Write failing Markdown tests**

Assert every HTTP(S) link is actionable, draft links get `.reader-link-draft`, fetched links expose state, no mailbox button renders, discovery reports normalized URL/full title, and a partial Selection opens the configured action.

- [ ] **Step 2: Write failing immersive-mode test**

Render ReadingLayout with reader action context, partially select a content link, and assert the same menu action appears.

- [ ] **Step 3: Verify RED**

```bash
npm test -- test/MarkdownArticleLinkActions.test.tsx test/ReadingLayoutLinkActions.test.tsx
```

Expected: assertions fail because the old mailbox/context path remains.

- [ ] **Step 4: Replace anchor/root rendering**

Create module-scoped `ArticleLink` using `ReaderActionContext`. Normalize href, derive `draft/fetched` state, report discovered title in an effect, and render only `<a>` with `className="reader-link-draft"` for drafts plus `data-reader-link-state`. Remove `LinkSetContext`, `LinkSetMarkIcon`, and wrapping span. Replace the `.markdown-body` root div with `ReaderInteractionSurface`.

- [ ] **Step 5: Wire ReadingLayout**

Add optional `readerActionContext?: ReaderActionContextValue` and wrap its MarkdownArticle with the provider. Keep tap-body exclusions unchanged.

- [ ] **Step 6: Add styles**

```css
.markdown-body a.reader-link-draft {
  text-decoration-line: underline;
  text-decoration-style: dashed;
  text-decoration-thickness: 2px;
  text-underline-offset: 4px;
  background: color-mix(in srgb, var(--accent) 6%, transparent);
}
.markdown-body a.reader-context-target {
  background: color-mix(in srgb, var(--accent) 18%, transparent);
  outline: 2px solid color-mix(in srgb, var(--accent) 55%, transparent);
  outline-offset: 2px;
}
```

Add menu positioning/focus/reduced-motion CSS with z-index below modal and above FAB.

- [ ] **Step 7: Verify and commit**

```bash
npm test -- test/MarkdownArticleLinkActions.test.tsx test/ReadingLayoutLinkActions.test.tsx
npm run build
git add src/components/MarkdownArticle.tsx src/components/ReadingLayout.tsx src/index.css test/MarkdownArticleLinkActions.test.tsx test/ReadingLayoutLinkActions.test.tsx
git commit -m "feat: render link actions in both reader modes"
```

## Task 12: Refactor confirmation around drafts

**Files:**
- Modify: `frontend/src/components/BatchFetchConfirmDialog.tsx`
- Create: `frontend/test/BatchFetchConfirmDialog.test.tsx`

- [ ] **Step 1: Write failing dialog tests**

Mock `batchFetchCandidates`. Assert ordered rows, default checks, all/invert/none, permanent remove, fetched disabling, payload, submitted-URL callback, and rejection retaining the open dialog/error/state.

```tsx
expect(batchFetchCandidates).toHaveBeenCalledWith(7, [{ title: 'A', url: 'https://a.example' }])
expect(onFetched).toHaveBeenCalledWith(['https://a.example'], 1)
```

- [ ] **Step 2: Verify RED**

```bash
npm test -- test/BatchFetchConfirmDialog.test.tsx
```

Expected: type/assertion failures because current props use candidates plus marked URLs.

- [ ] **Step 3: Implement draft props**

```ts
interface Props {
  open: boolean
  articleId: number
  links: DraftLink[]
  fetchedURLs: Set<string>
  onRemove: (url: string) => void
  onClose: () => void
  onFetched: (submittedURLs: string[], insertedCount: number) => void | Promise<void>
}
```

Rows retain draft order. Initialize checks on false-to-true open, await `onFetched` before close, and update empty copy to instruct text selection rather than mailbox clicks.

- [ ] **Step 4: Verify and commit**

```bash
npm test -- test/BatchFetchConfirmDialog.test.tsx
git add src/components/BatchFetchConfirmDialog.tsx test/BatchFetchConfirmDialog.test.tsx
git commit -m "feat: confirm selected link drafts"
```

## Task 13: Integrate drafts/actions into ArticlePage and delete mailbox UI

**Files:**
- Modify: `frontend/src/utils/linkSetSelection.ts`
- Modify: `frontend/src/pages/ArticlePage.tsx`
- Modify: `frontend/src/components/ReadingLayout.tsx`
- Delete: `frontend/src/components/LinkSetMarkIcon.tsx`
- Delete: `frontend/src/components/LinkSetContext.tsx`
- Create: `frontend/test/articleLinkSetState.test.ts`

- [ ] **Step 1: Write failing immutable transition tests**

Add/test `addDraftTargets`, `removeDraftURLs`, and `enrichDraftLinks`. They preserve order, ignore fetched/duplicate targets, enrich only fallback titles, clear only submitted URLs, and preserve array identity when unchanged.

```ts
expect(addDraftTargets(existing, [a, duplicateA, b], fetched)).toEqual([existing[0], aDraft, bDraft])
expect(removeDraftURLs([aDraft, bDraft], new Set([aDraft.url]))).toEqual([bDraft])
```

- [ ] **Step 2: Verify RED**

```bash
npm test -- test/articleLinkSetState.test.ts
```

Expected: compile failure for missing transition helpers.

- [ ] **Step 3: Implement transition helpers**

Add the three pure functions to `linkSetSelection.ts`; use normalized URLs and immutable updates.

- [ ] **Step 4: Replace ArticlePage state**

Remove candidates, marked URL Set, candidate-fetch effect, LinkSetContext, and confirm conversion imports. Add ordered drafts loaded on every article ID regardless of `links_extendable`; persist changes. Derive draft/fetched URL Sets, then memoize `ReaderActionContextValue` using `buildLinkSetActions`.

Callbacks: add shows Toast; remove deletes URLs; open uses `window.open(url, '_blank', 'noopener,noreferrer')`; copy uses Clipboard with error Toast; discovery enriches migrated titles.

- [ ] **Step 5: Wire both modes, FAB, dialog, and children**

- Normal mode wraps MarkdownArticle with `ReaderActionContext.Provider`.
- Reading mode receives `readerActionContext`.
- FAB renders only for non-empty drafts with label `待抓取 N`.
- Remove `转为 link_set` FAB.
- Dialog receives drafts/fetched Set; on success remove only submitted URLs, then refresh article/children. Refresh failure does not restore submitted drafts.
- Render `LinkSetChildren` when `article.links_extendable === true || children.length > 0`.

- [ ] **Step 6: Delete obsolete files**

After `rg 'LinkSetMarkIcon|LinkSetContext' frontend/src` finds only definitions, delete both components. Keep candidate API methods for compatibility.

- [ ] **Step 7: Verify and commit**

```bash
npm test -- test/articleLinkSetState.test.ts
npm run check
npm run build
rg -n '转为 link_set|📪|📫|📬|LinkSetMarkIcon|LinkSetContext' src || true
git add src/pages/ArticlePage.tsx src/components/ReadingLayout.tsx src/utils/linkSetSelection.ts test/articleLinkSetState.test.ts
git add -u src/components/LinkSetMarkIcon.tsx src/components/LinkSetContext.tsx
git commit -m "feat: select article links without explicit conversion"
```

Expected: tests/build pass and the search prints no production matches.

## Task 14: Full verification and manual acceptance

**Files:**
- Modify: `docs/superpowers/plans/2026-07-21-link-selection-context-menu.md` only when checking completed steps.
- Verify: `docs/superpowers/specs/2026-07-21-link-selection-context-menu-design.md`.

- [ ] **Step 1: Run frontend verification**

```bash
cd frontend
npm run check
npm run build
```

Expected: all Vitest/CJS checks pass and Vite exits 0. Existing large-chunk warning is acceptable; TypeScript errors are not.

- [ ] **Step 2: Run backend verification**

```bash
cd ../backend
go test ./internal/rss ./internal/api ./internal/repository -count=1
go test ./... -count=1
```

Expected: all packages PASS; DB-dependent tests may SKIP only if PostgreSQL is unavailable.

- [ ] **Step 3: Run static contract checks**

```bash
cd ..
rg -n '转为 link_set|📪|📫|📬|LinkSetMarkIcon|LinkSetContext' frontend/src || true
rg -n 'preventDefault\(\)' frontend/src/reader
git diff --check
git status --short
```

Inspect every `preventDefault()` match: none may unconditionally handle desktop mouse `contextmenu`; one-shot touch suppression and menu pointerdown are allowed.

- [ ] **Step 4: Manual browser acceptance**

Verify all ten scenarios:

1. selecting middle characters of one link highlights the complete link and opens the menu;
2. selecting across two links highlights both complete links with correct counts;
3. selecting plain text opens nothing;
4. right-click uses the browser native menu;
5. mobile short tap navigates, 500ms long press opens, drag does not;
6. adding creates dashed style/count FAB and survives reload;
7. first submit has no conversion prompt and children appear without hard refresh;
8. child processing state updates without stale 304 behavior;
9. failure retains drafts and partial submission retains unchecked links;
10. normal and immersive modes share state/actions.

- [ ] **Step 5: Record verification if the plan checkboxes changed**

```bash
git add docs/superpowers/plans/2026-07-21-link-selection-context-menu.md
git commit -m "docs: record link selection verification"
```

Skip this commit rather than creating an empty commit.
