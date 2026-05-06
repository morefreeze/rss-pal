# Bookmarklet Rescrape + Duplicate UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the destructive "重新抓取" button on bookmarklet articles with an "open original page" flow, and add an explicit overwrite/keep prompt when the bookmarklet captures a known URL with non-improving content.

**Architecture:** Backend exposes a derived `from_bookmarklet` boolean on `GET /api/articles/:id` (computed from `feeds.feed_type='saved'`). The bookmarklet capture endpoint accepts a `force` field and a 1.5× length threshold gates auto-overwrite; below that, returns `status:"duplicate"` with content-length info. Frontend swaps the button text/handler when `from_bookmarklet` is true (opens the source URL in a new tab and prompts the user to click their bookmark), and the bookmarklet receiver page renders an overwrite/keep choice when it sees a duplicate response.

**Tech Stack:**
- Backend: Go 1.24+, gin, `database/sql`, lib/pq (existing). New code is in `internal/api`.
- Frontend: React 18 + TypeScript, Vite (existing). One static HTML file (`bookmarklet-receiver.html`) carries the receiver logic.
- Source spec: `docs/superpowers/specs/2026-05-06-bookmarklet-rescrape-and-dedup-design.md`

---

## File Structure

**Modified backend files:**
- `backend/internal/repository/article.go` — add `GetByIDWithFeedType` (parallel to `GetByID`, also returns feed.feed_type)
- `backend/internal/api/article.go` — `GetByID` handler reads feed_type alongside the article and includes `from_bookmarklet` in the response
- `backend/internal/api/bookmarklet.go` — add `force` field, duplicate-threshold logic, extract `shouldPromptDuplicate` helper

**New backend files:**
- `backend/internal/api/bookmarklet_test.go` — table-driven test for `shouldPromptDuplicate`

**Modified frontend files:**
- `frontend/src/api/client.ts` — `Article` type gains `from_bookmarklet?: boolean`
- `frontend/src/pages/ArticlePage.tsx` — button branch on `from_bookmarklet`; new handler that opens source URL
- `frontend/public/bookmarklet-receiver.html` — extract `postCapture` + `handleResponse`; add `phase==='duplicate'` branch with two buttons

**Out of scope:** schema changes, RSS-side dedup behavior changes, Modal component, save-as-new copies of an article.

---

## Phase 1 — Backend: expose `from_bookmarklet`

### Task 1: Repo helper `GetByIDWithFeedType`

**Files:**
- Modify: `backend/internal/repository/article.go`

Adds a method that returns the article PLUS its feed's `feed_type` string, so the API layer can derive `from_bookmarklet` without touching `model.Article`.

- [ ] **Step 1: Add the method**

Open `backend/internal/repository/article.go`. Find the existing `GetByID` (around line 107). Below it, add:

```go
// GetByIDWithFeedType returns the article alongside its feed's feed_type
// (e.g., "rss" / "saved" / "youtube"). Used by the article handler to derive
// the from_bookmarklet response field without modifying model.Article.
func (r *ArticleRepository) GetByIDWithFeedType(id, userID int) (*model.Article, string, error) {
	query := `
		SELECT a.id, a.feed_id, a.title, a.url, a.content, a.published_at, a.summary_brief, a.summary_detailed, a.fetched_at, a.word_count, a.reading_minutes, f.title as feed_title, f.feed_type
		FROM articles a
		JOIN feeds f ON a.feed_id = f.id
		WHERE a.id = $1 AND (f.owner_id IS NULL OR f.owner_id = $2)`
	var a model.Article
	var content, summaryBrief, summaryDetailed, feedTitle, feedType sql.NullString
	err := r.db.QueryRow(query, id, userID).Scan(
		&a.ID, &a.FeedID, &a.Title, &a.URL, &content, &a.PublishedAt,
		&summaryBrief, &summaryDetailed, &a.FetchedAt, &a.WordCount, &a.ReadingMinutes,
		&feedTitle, &feedType,
	)
	if err != nil {
		return nil, "", err
	}
	a.Content = content.String
	a.SummaryBrief = summaryBrief.String
	a.SummaryDetailed = summaryDetailed.String
	a.FeedTitle = feedTitle.String
	return &a, feedType.String, nil
}
```

- [ ] **Step 2: Verify compile**

```bash
cd backend
go1.25.3 build ./...
```

Expected: clean (no output).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/repository/article.go
git commit -m "feat(repo): add GetByIDWithFeedType for from_bookmarklet derivation"
```

> No unit test for the repo method. Project has no repository test harness; verification happens via the handler in the next task.

---

### Task 2: Article handler returns `from_bookmarklet`

**Files:**
- Modify: `backend/internal/api/article.go`

- [ ] **Step 1: Update `GetByID` to use the new repo method**

Open `backend/internal/api/article.go`. Find `func (h *ArticleHandler) GetByID(c *gin.Context)` (around line 98).

Replace the call to `h.articleRepo.GetByID(...)` and the response assembly with this version:

```go
func (h *ArticleHandler) GetByID(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	article, feedType, err := h.articleRepo.GetByIDWithFeedType(id, getUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
		return
	}

	userID := getUserID(c)
	progress, _ := h.progressRepo.GetByArticleAndUser(id, userID)
	signals, _ := h.prefRepo.GetUserSignals(userID, id)

	response := gin.H{
		"article":          article,
		"progress":         progress,
		"signals":          signals,
		"from_bookmarklet": feedType == "saved",
	}
	c.JSON(http.StatusOK, response)
}
```

- [ ] **Step 2: Verify compile**

```bash
cd backend
go1.25.3 build ./...
```

Expected: clean.

- [ ] **Step 3: Verify existing tests still pass**

```bash
cd backend
go1.25.3 test ./internal/api/ ./internal/rss/ -v
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/article.go
git commit -m "feat(api): expose from_bookmarklet on GET /api/articles/:id

Derived from feeds.feed_type='saved'. Frontend uses it to swap the
'重新抓取' button for bookmarklet-captured articles."
```

---

## Phase 2 — Backend: bookmarklet duplicate prompt

### Task 3: `shouldPromptDuplicate` helper + handler integration

**Files:**
- Modify: `backend/internal/api/bookmarklet.go`
- Create: `backend/internal/api/bookmarklet_test.go`

Extract the threshold decision into a pure function so it can be unit-tested without a DB harness. Then wire it into the `Capture` handler alongside the `force` field.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/api/bookmarklet_test.go`:

```go
package api

import "testing"

func TestShouldPromptDuplicate(t *testing.T) {
	cases := []struct {
		name     string
		newLen   int
		oldLen   int
		force    bool
		expected bool
	}{
		// force always wins
		{"force overrides everything", 100, 1000, true, false},
		{"force on improvement still passes through", 5000, 1000, true, false},

		// oldLen == 0 means no real prior content; auto-overwrite
		{"old empty, any new", 0, 0, false, false},
		{"old empty, new has content", 100, 0, false, false},

		// 1.5x boundary: clear improvement skips prompt
		{"new exactly 1.5x triggers no prompt", 1500, 1000, false, false},
		{"new just above 1.5x", 1501, 1000, false, false},
		{"new far above 1.5x", 5000, 1000, false, false},

		// below 1.5x prompts
		{"new just below 1.5x prompts", 1499, 1000, false, true},
		{"new equal to old prompts", 1000, 1000, false, true},
		{"new shorter than old prompts", 500, 1000, false, true},
		{"new much shorter prompts", 100, 1000, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldPromptDuplicate(tc.newLen, tc.oldLen, tc.force)
			if got != tc.expected {
				t.Errorf("shouldPromptDuplicate(new=%d, old=%d, force=%v) = %v, want %v",
					tc.newLen, tc.oldLen, tc.force, got, tc.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend
go1.25.3 test ./internal/api/ -run TestShouldPromptDuplicate -v
```

Expected: FAIL — `shouldPromptDuplicate` is undefined.

- [ ] **Step 3: Implement the helper**

Open `backend/internal/api/bookmarklet.go`. Near the top of the file (after the `captureMaxBodyBytes` const, before `type BookmarkletHandler`), add:

```go
// duplicateOverwriteRatio is the threshold at which a re-captured article's
// new content is considered a clear improvement and we silently overwrite.
// Below this ratio, the receiver page asks the user to confirm.
const duplicateOverwriteRatio = 1.5

// shouldPromptDuplicate returns true when a bookmarklet capture for an
// existing URL should pause and ask the user (rather than auto-overwriting).
// Pure function so it can be unit-tested without a DB. The caller passes
// the lengths of the new and stored content; force=true bypasses the prompt
// entirely (used after the user has explicitly chosen to overwrite).
func shouldPromptDuplicate(newLen, oldLen int, force bool) bool {
	if force {
		return false
	}
	if oldLen == 0 {
		return false
	}
	return float64(newLen) < duplicateOverwriteRatio*float64(oldLen)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend
go1.25.3 test ./internal/api/ -run TestShouldPromptDuplicate -v
```

Expected: 11 sub-tests PASS.

- [ ] **Step 5: Wire helper into `Capture` handler**

Still in `backend/internal/api/bookmarklet.go`, find the request body decode struct (around line 55):

```go
var req struct {
    URL   string `json:"url"`
    Title string `json:"title"`
    HTML  string `json:"html"`
}
```

Add a `Force` field:

```go
var req struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	HTML  string `json:"html"`
	Force bool   `json:"force"`
}
```

Then find the existing duplicate-handling block (currently around lines 94-120, beginning with `if existing != nil {` and ending with `return` after the "updated" JSON response). Replace the entire `if existing != nil { ... }` block with:

```go
	if existing != nil {
		newLen, oldLen := len(content), len(existing.Content)
		if shouldPromptDuplicate(newLen, oldLen, req.Force) {
			c.JSON(http.StatusOK, gin.H{
				"status":          "duplicate",
				"article_id":      existing.ID,
				"existing_length": oldLen,
				"new_length":      newLen,
				"message":         fmt.Sprintf("已有内容 %d 字 / 新内容 %d 字", oldLen, newLen),
			})
			return
		}
		wc, rm := rss.ComputeMetrics(content)
		if err := h.articleRepo.UpdateContent(existing.ID, content, wc, rm); err != nil {
			log.Printf("bookmarklet: UpdateContent failed for article=%d: %v", existing.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新文章失败"})
			return
		}
		// Clearing summaries forces the worker's backfillSummaries loop to
		// regenerate them from the new content on its next pass.
		if err := h.articleRepo.UpdateSummary(existing.ID, "", ""); err != nil {
			log.Printf("bookmarklet: clear summary failed for article=%d: %v", existing.ID, err)
		}
		log.Printf("bookmarklet: updated article=%d user=%d url=%s len=%d (force=%v)", existing.ID, user.ID, normalized, newLen, req.Force)
		c.JSON(http.StatusOK, gin.H{
			"status":     "updated",
			"article_id": existing.ID,
			"message":    "已更新文章: " + existing.Title,
		})
		return
	}
```

The `unchanged` status is now gone — duplicate-but-not-improving cases return `duplicate` instead, and the user resolves them on the receiver page.

Verify the file uses `fmt` already (it should). If not, add `"fmt"` to the import block.

- [ ] **Step 6: Verify build**

```bash
cd backend
go1.25.3 build ./...
go1.25.3 vet ./...
```

Expected: both clean.

- [ ] **Step 7: Run full api tests**

```bash
cd backend
go1.25.3 test ./internal/api/ -v
```

Expected: all tests PASS, including the new `TestShouldPromptDuplicate` and the existing proxy tests.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/api/bookmarklet.go backend/internal/api/bookmarklet_test.go
git commit -m "feat(bookmarklet): duplicate prompt + force overwrite

When a re-capture's new content is below 1.5x the existing length
(and force=false), respond with status:'duplicate' instead of
auto-overwriting. The receiver page surfaces a 覆盖 / 保留旧内容
choice; 'covers' re-POSTs with force:true.

Removes the prior 'unchanged' silent-skip branch — duplicate-but-
not-improving cases are now visible to the user."
```

---

## Phase 3 — Frontend: Article type + button branch

### Task 4: Add `from_bookmarklet` to `Article` type

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Locate the Article type**

Open `frontend/src/api/client.ts`. Search for `export type Article` or `export interface Article` (it's the type used by `getArticle()` / `Article` consumers in pages).

- [ ] **Step 2: Add the optional field**

Add `from_bookmarklet?: boolean` to the type definition. Order: alongside other top-level fields. Example (adapt to actual existing fields):

```ts
export type Article = {
  id: number
  feed_id: number
  feed_title?: string
  title: string
  url: string
  content: string
  // ...other existing fields...
  from_bookmarklet?: boolean
}
```

If the GET response is wrapped in a struct that returns `{article, progress, signals}`, also extend that wrapper to include `from_bookmarklet?: boolean` at the top level — and update `getArticle` to surface it. The shape should mirror the backend response (`gin.H{"article": ..., "progress": ..., "signals": ..., "from_bookmarklet": bool}`).

Concretely: find the return-type annotation of `getArticle`. It probably looks like `{ article: Article; progress: ...; signals: ... }`. Add `from_bookmarklet?: boolean` at the wrapper level so callers can read `data.from_bookmarklet` rather than `data.article.from_bookmarklet`.

(If the wrapper is anonymous/inferred and you can't pin it down quickly, fall back to adding `from_bookmarklet?` to `Article` itself — backend can be adapted later. Note in your report whichever shape you used.)

- [ ] **Step 3: Type-check**

```bash
cd frontend
npx tsc --noEmit
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(frontend): Article response gains from_bookmarklet flag"
```

---

### Task 5: ArticlePage button branch + rescrape handler

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 1: Read the current state of `loadArticle`**

In `frontend/src/pages/ArticlePage.tsx`, find `loadArticle` (around line 45). It calls `getArticle(Number(id))` and stores the result. We need access to `data.from_bookmarklet` (or `data.article.from_bookmarklet`, depending on which shape Task 4 chose).

Add a state variable near the other useState declarations (top of the component):

```tsx
const [fromBookmarklet, setFromBookmarklet] = useState(false)
```

Inside `loadArticle`, after `setArticle(data.article)`, set the new flag. The exact line depends on Task 4's choice:

```tsx
setFromBookmarklet(Boolean(data.from_bookmarklet))
```

(or `Boolean(data.article.from_bookmarklet)` if that was the chosen shape).

- [ ] **Step 2: Add the rescrape-via-bookmarklet handler**

Add this handler near the existing `handleFetchContent`:

```tsx
const handleRescrapeViaBookmarklet = () => {
  if (!article) return
  const ok = window.confirm(
    `重新抓取需要在原网页运行书签。\n` +
    `会打开 ${article.url}，请到新标签页点你 bookmark bar 上的 RSS Pal 书签来抓取最新内容。\n\n` +
    `继续？`
  )
  if (!ok) return
  window.open(article.url, '_blank', 'noopener,noreferrer')
  toast.info('已打开原网页 — 在新标签里点你的 RSS Pal 书签')
}
```

If `toast` doesn't have an `info` method, use `toast.success` instead (check `frontend/src/utils/toast.ts` for the available levels). The choice is informational, not blocking.

- [ ] **Step 3: Branch the existing button**

Find the existing "重新抓取" button. It's inside the "原文内容" card, with `onClick={handleFetchContent}`:

```tsx
<button onClick={handleFetchContent} disabled={fetchingContent}>
  {fetchingContent ? '获取中...' : '重新抓取'}
</button>
```

Replace with:

```tsx
{fromBookmarklet ? (
  <button onClick={handleRescrapeViaBookmarklet} title="在新标签打开原网页，由你点击书签来更新">
    🔁 通过书签重新抓取
  </button>
) : (
  <button onClick={handleFetchContent} disabled={fetchingContent}>
    {fetchingContent ? '获取中...' : '重新抓取'}
  </button>
)}
```

- [ ] **Step 4: Type-check**

```bash
cd frontend
npx tsc --noEmit
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/ArticlePage.tsx
git commit -m "feat(frontend): rescrape via bookmarklet for from_bookmarklet articles

Bookmarklet-captured articles now show '🔁 通过书签重新抓取' that
opens the source URL in a new tab and prompts the user to click
their bookmarklet — the only flow that can reach login-walled or
JS-rendered pages without destroying the captured content."
```

---

## Phase 4 — Frontend: bookmarklet-receiver duplicate UI

### Task 6: `bookmarklet-receiver.html` duplicate branch

**Files:**
- Modify: `frontend/public/bookmarklet-receiver.html`

This is a static HTML file shipped from `public/` (Vite copies it into `dist/` on build). The existing fetch lives inside the `onMsg` handler, which has the captured `d` (token, url, title, html) in closure.

We refactor so `postCapture(force)` and `handleResponse(x)` both close over `d`, then add a `duplicate` branch.

- [ ] **Step 1: Refactor the existing fetch into reusable functions**

Open `frontend/public/bookmarklet-receiver.html`. Inside the `onMsg(e)` function, replace the entire current fetch block (the `fetch('/api/bookmarklet/capture', ...)` chain through its `.then(function (x) { ... })` and `.catch(...)`) with:

```js
    function postCapture(force) {
      return fetch('/api/bookmarklet/capture', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': 'Bearer ' + d.token,
        },
        body: JSON.stringify({
          url: d.url,
          title: d.title || '',
          html: d.html,
          force: !!force,
        }),
      })
        .then(function (r) {
          return r.json().then(function (j) { return { ok: r.ok, j: j, status: r.status }; });
        });
    }

    function handleResponse(x) {
      if (!x.ok) {
        fail(x.j && x.j.error ? x.j.error : 'HTTP ' + x.status);
        return;
      }
      var phase = x.j.status;
      if (phase === 'duplicate') {
        renderDuplicatePrompt(x.j);
        return;
      }
      var heading = phase === 'created' ? '✅ 已收藏'
        : phase === 'updated' ? '✅ 已更新'
        : '✅ 抓取成功';
      var cls = 'ok';
      render(cls, heading, escapeText(x.j.message), true);
    }

    function renderDuplicatePrompt(j) {
      box.innerHTML =
        '<h1 class="info">⚠️ 已有同 URL 文章</h1>'
        + '<p>现有内容 ' + j.existing_length + ' 字 / 新内容 ' + j.new_length + ' 字</p>'
        + '<p>新内容看起来不是明显改进，怎么处理？</p>'
        + '<div style="margin-top:14px;display:flex;gap:8px;justify-content:center;">'
        +   '<button id="ow" style="padding:8px 18px;background:#2563eb;color:#fff;border:none;border-radius:6px;cursor:pointer;">覆盖</button>'
        +   '<button id="kp" style="padding:8px 18px;background:#f3f4f6;border:1px solid #ddd;border-radius:6px;cursor:pointer;">保留旧内容</button>'
        + '</div>';
      document.getElementById('ow').onclick = function () {
        box.innerHTML = '<h1 class="info"><span class="spinner"></span>正在覆盖…</h1>';
        postCapture(true).then(handleResponse).catch(function (err) { fail('网络错误: ' + err.message); });
      };
      document.getElementById('kp').onclick = function () {
        render('info', 'ℹ️ 已保留旧内容', '可关闭此页面', false);
      };
    }

    box.innerHTML = '<h1 class="info"><span class="spinner"></span>正在保存…</h1>';
    postCapture(false).then(handleResponse).catch(function (err) { fail('网络错误: ' + err.message); });
```

The block must remain inside `onMsg(e)` so the closure captures `d`, `box`, `render`, `fail`, `escapeText`. Don't move it to outer scope.

The previous `unchanged` heading branch is removed — the backend no longer returns that status. If a stale backend somehow still does, it falls into the default "✅ 抓取成功" branch (acceptable degradation).

- [ ] **Step 2: Inspect the result**

Open the file and confirm:
- The fetch is no longer inline; it's only called via `postCapture(false)` and (inside the duplicate branch) `postCapture(true)`.
- `handleResponse` is the single handler for all server responses.
- The whole block is still inside `function onMsg(e) { ... }`.

- [ ] **Step 3: (Optional) Manual sanity in dev**

If your dev environment serves this file via Vite, you can preview by:

```bash
cd frontend
npm run dev
```

Visit `http://localhost:5173/bookmarklet-receiver.html` directly — you should see the "等待页面数据…" message (because there's no opener). That's the expected initial state and confirms the file still parses.

If the Vite dev server isn't trivially available, skip this step — Task 7 covers full-flow verification.

- [ ] **Step 4: Commit**

```bash
git add frontend/public/bookmarklet-receiver.html
git commit -m "feat(bookmarklet-receiver): duplicate prompt UI

Refactor the fetch into postCapture(force) + handleResponse(x). On
status:'duplicate', show the existing/new content lengths and two
buttons: 覆盖 re-POSTs with force:true; 保留旧内容 closes with no
DB write. Removes the unchanged branch (backend no longer emits it)."
```

---

## Phase 5 — End-to-end verification

### Task 7: Full smoke test

**Files:** none (operational)

This task verifies the change end-to-end and is the only place we exercise the actual browser flow.

- [ ] **Step 1: Rebuild and start the stack**

```bash
cd /Users/bytedance/mygit/rss-pal
docker-compose up -d --build api worker frontend
```

Wait for healthchecks to settle (or `docker-compose logs -f api` to see the server start line).

- [ ] **Step 2: Verify a non-bookmarklet article**

Open any RSS-feed article in the UI. Expected:
- The button under "原文内容" still says "重新抓取".
- Clicking it runs the existing flow (re-fetches via backend HTTP and updates content).

- [ ] **Step 3: Verify a bookmarklet article**

Open a bookmarklet-captured article (one in the "📑 收藏" feed). Expected:
- The button now says "🔁 通过书签重新抓取".
- Clicking it shows a `confirm()` dialog with the source URL.
- Confirming opens the source URL in a new tab AND shows a toast in the original tab.
- Cancelling does nothing.

- [ ] **Step 4: Verify duplicate auto-overwrite (≥1.5×)**

Pick any bookmarklet article whose stored content is short (e.g., <500 chars). On the source page, click your RSS Pal bookmarklet. Expected:
- The receiver popup goes from "正在保存…" straight to "✅ 已更新".

If you don't have a convenient short-content article, skip this and rely on the unit test from Task 3 — it covers the threshold cases.

- [ ] **Step 5: Verify duplicate prompt (<1.5×)**

Find a bookmarklet article that captured well (e.g., 1500+ chars on a normal page). Then visit a page that returns less content for the same URL — for example, a logged-out version of a site behind a paywall, or simply a page you've already captured at full content. Click the bookmarklet. Expected:
- The receiver shows "⚠️ 已有同 URL 文章 / 现有内容 N 字 / 新内容 M 字" with two buttons.
- Click "保留旧内容" → page shows "ℹ️ 已保留旧内容". Verify in the article list/detail that content is unchanged.
- Recapture and click "覆盖" → page shows "正在覆盖…" then "✅ 已更新". Verify the article content is now the new (shorter) one.

- [ ] **Step 6: Backend test re-run**

```bash
cd backend
go1.25.3 test ./...
```

Expected: all PASS, including `TestShouldPromptDuplicate`.

- [ ] **Step 7: Frontend build re-run**

```bash
cd frontend
npm run build
```

Expected: success (TypeScript compile + Vite bundle).

- [ ] **Step 8: Confirm the commit graph**

```bash
cd /Users/bytedance/mygit/rss-pal
git log --oneline a06d4ec..HEAD
```

Expected: 1 spec commit + 6 task commits = 7 total. Each commit clearly describes one logical change.

If everything checks out, the branch is ready for finishing-a-development-branch.

---

## Self-Review Notes

**Spec coverage:**
- §3 decision 1 (button replaced for bookmarklet articles): Tasks 4–5
- §3 decision 2 (origin via `feed_type='saved'`): Tasks 1–2
- §3 decision 3 (覆盖/保留旧内容 only): Task 6 (UI shows only those two)
- §3 decision 4 (1.5× threshold): Task 3 (helper + tests)
- §3 decision 5 (`force` in body): Task 3 (handler) + Task 6 (`postCapture(true)`)
- §5.1 article handler `from_bookmarklet`: Task 2
- §5.2 bookmarklet capture rewrite: Task 3
- §5.3 tests: Task 3
- §6.1 frontend type: Task 4
- §6.2 frontend button: Task 5
- §6.3 receiver page: Task 6
- §7 deploy order — Tasks land backend first (1–3), frontend after (4–6); compatible with the spec's rolling-deploy expectation.

**Type / name consistency:**
- `shouldPromptDuplicate(newLen, oldLen, force)` signature matches its test cases and call site.
- `from_bookmarklet` (snake_case JSON field) ↔ `fromBookmarklet` (TS state) used consistently.
- `postCapture(force)` always takes a boolean and always goes to the same endpoint.

**No placeholders / TBD spotted.**
