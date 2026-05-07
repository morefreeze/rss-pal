# Article Page Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Replace `navigate(-1)` on the article page's 返回 button with explicit entry-path tracking; cap nav list to ±50; switch prev/next to `replace`.

**Spec:** `docs/superpowers/specs/2026-05-07-article-nav-entry-path-design.md`

---

### Task 1: ArticleListPage — capped navList + entryPath + state.from

**File:** `frontend/src/pages/ArticleListPage.tsx`

- [ ] **Step 1:** Locate the existing click code (around line 215–219) that sets `articleNavList` from `articles.map(a => a.id)` and calls `navigate(\`/articles/${id}\`)`. Replace it with the capped+stateful version below.

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

Update the `onClick` (and any keyboard "open selected" path) to call `handleArticleClick(article.id)`.

- [ ] **Step 2:** `cd frontend && npx tsc --noEmit`. Expect no errors.

- [ ] **Step 3:** Commit `feat(article-nav): cap navList to ±50 + record entryPath on list click`.

---

### Task 2: RecommendationsCard — clear navList + entryPath='/insights'

**File:** `frontend/src/components/RecommendationsCard.tsx`

- [ ] **Step 1:** Replace the existing `onClick={() => navigate(\`/articles/${a.article_id}\`)}` with:

```tsx
onClick={() => {
  sessionStorage.removeItem('articleNavList')
  sessionStorage.setItem('articleEntryPath', '/insights')
  navigate(`/articles/${a.article_id}`, { state: { from: '/insights' } })
}}
```

- [ ] **Step 2:** `cd frontend && npx tsc --noEmit`. Expect no errors.

- [ ] **Step 3:** Commit `feat(article-nav): rec card sets entryPath to /insights and clears navList`.

---

### Task 3: ArticlePage — entryPath + handleBack + replace-based prev/next

**File:** `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 1:** Add `useLocation` to the existing react-router-dom import.

- [ ] **Step 2:** Inside the component, after `useNavigate()`, add:

```ts
const location = useLocation()
const entryPath =
  (location.state as { from?: string } | null)?.from
  ?? sessionStorage.getItem('articleEntryPath')
  ?? '/articles'
const handleBack = () => navigate(entryPath)
```

- [ ] **Step 3:** Replace **all three** `navigate(-1)` call sites (the keyboard shortcut path around line 127, the empty-state 返回 button around line 450, and the main 返回 button around line 505) with `handleBack`.

- [ ] **Step 4:** Replace the prev/next button click handlers and the keyboard `j`/`k` (or whatever) navigation to use `replace: true` and forward state:

```tsx
// prev button
onClick={() => navigate(`/articles/${prevId}`, { replace: true, state: { from: entryPath } })}
// next button
onClick={() => navigate(`/articles/${nextId}`, { replace: true, state: { from: entryPath } })}
```

In the keyboard handler, the corresponding lines that do `navigate(\`/articles/${nextId}\`)` and `navigate(\`/articles/${prevId}\`)` get the same `{ replace: true, state: { from: entryPath } }` second arg.

- [ ] **Step 5:** `cd frontend && npx tsc --noEmit`. Expect no errors.

- [ ] **Step 6:** Commit `feat(article-nav): explicit entryPath back + replace-based prev/next`.

---

### Task 4: Smoke test + push

- [ ] **Step 1:** Rebuild frontend container (no backend changes):

```bash
docker-compose up -d --build frontend
```

- [ ] **Step 2:** Manual smoke per spec testing section: list → A → next → B → 返回; rec card → A → 返回; direct URL paste; refresh on article.

- [ ] **Step 3:** Push to existing branch (PR #5 auto-updates):

```bash
git push origin feature/insights-full
```
