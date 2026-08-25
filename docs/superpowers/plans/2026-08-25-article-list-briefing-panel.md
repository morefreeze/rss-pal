# Article List Briefing Panel Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `/articles` recommendation panel with a collapsed daily briefing panel whose rows show accurate per-user read state.

**Architecture:** Reuse the existing daily-digest API and the existing `rec-panel` presentation inside `ArticleListPage`. Keep the change frontend-only: replace recommendation state/effects/feedback handlers with digest state, and combine API `is_read` with the page's existing session read-ID set when rendering briefing rows.

**Tech Stack:** React 18, TypeScript, React Router, Vitest, Testing Library

---

### Task 1: Specify the briefing panel behavior

**Files:**
- Modify: `frontend/test/ArticleListPageInfiniteScroll.test.tsx`

- [ ] **Step 1: Add the daily-digest mock and default response**

Add `getDailyDigest: vi.fn()` to `apiMocks`, then replace the default recommendation response in `beforeEach` with:

```tsx
apiMocks.getDailyDigest.mockResolvedValue({
  requested_date: '2026-08-25',
  shown_date: '2026-08-25',
  pending: false,
  intro_text: '',
  articles: [],
  mode: 'cached',
})
```

Keep `getRecommended` mocked only so the regression test can prove it is no longer called.

- [ ] **Step 2: Add a failing collapsed-panel test**

Add this test inside the existing `ArticleListPage automatic pagination` suite:

```tsx
it('replaces recommendations with a collapsed daily briefing panel', async () => {
  apiMocks.getArticles.mockResolvedValue([])
  apiMocks.getDailyDigest.mockResolvedValue({
    requested_date: '2026-08-25',
    shown_date: '2026-08-25',
    pending: false,
    intro_text: '',
    articles: [makeBriefingArticle(101, 'Daily briefing article')],
    mode: 'cached',
  })

  render(
    <MemoryRouter initialEntries={['/articles']}>
      <ArticleListPage />
    </MemoryRouter>,
  )

  const header = await screen.findByRole('button', { name: /简报/ })
  expect(header.getAttribute('aria-expanded')).toBe('false')
  expect(screen.queryByText('Daily briefing article')).toBeNull()
  expect(screen.queryByText('为你推荐')).toBeNull()
  expect(apiMocks.getRecommended).not.toHaveBeenCalled()

  fireEvent.click(header)
  expect(await screen.findByRole('link', { name: /Daily briefing article/ })).toBeTruthy()
})
```

- [ ] **Step 3: Add a failing read-state test**

Add `Article` to the existing type import and add this helper before the test
suite:

```tsx
function makeBriefingArticle(id: number, title: string, read = false): Article {
  return {
    id,
    feed_id: 1,
    feed_title: 'Briefing Feed',
    title,
    url: `https://example.com/${id}`,
    content: '',
    published_at: '2026-08-25T00:00:00Z',
    summary_brief: '',
    summary_detailed: '',
    fetched_at: '2026-08-25T00:00:00Z',
    is_read: read,
    manual_tags: [],
  }
}
```

Then add:

```tsx
it('shows the briefing articles current server and session read state', async () => {
  sessionStorage.setItem('readArticles', JSON.stringify([103]))
  apiMocks.getArticles.mockResolvedValue([])
  apiMocks.getDailyDigest.mockResolvedValue({
    requested_date: '2026-08-25',
    shown_date: '2026-08-25',
    pending: false,
    intro_text: '',
    articles: [
      makeBriefingArticle(101, 'Unread briefing article', false),
      makeBriefingArticle(102, 'Server-read briefing article', true),
      makeBriefingArticle(103, 'Session-read briefing article', false),
    ],
    mode: 'cached',
  })

  render(
    <MemoryRouter initialEntries={['/articles']}>
      <ArticleListPage />
    </MemoryRouter>,
  )

  fireEvent.click(await screen.findByRole('button', { name: /简报/ }))
  const unread = await screen.findByRole('link', { name: /Unread briefing article/ })
  const serverRead = screen.getByRole('link', { name: /Server-read briefing article/ })
  const sessionRead = screen.getByRole('link', { name: /Session-read briefing article/ })

  expect(unread.style.opacity).toBe('1')
  expect(unread.querySelector('[aria-label="未读"]')).toBeTruthy()
  expect(serverRead.style.opacity).toBe('0.6')
  expect(serverRead.querySelector('[aria-label="未读"]')).toBeNull()
  expect(sessionRead.style.opacity).toBe('0.6')
  expect(sessionRead.querySelector('[aria-label="未读"]')).toBeNull()
})
```

- [ ] **Step 4: Run the focused tests and verify RED**

Run:

```bash
cd frontend && npm test -- ArticleListPageInfiniteScroll.test.tsx
```

Expected: FAIL because `ArticleListPage` does not request `getDailyDigest` and still renders the recommendation panel.

### Task 2: Replace recommendations with the daily briefing

**Files:**
- Modify: `frontend/src/pages/ArticleListPage.tsx`
- Test: `frontend/test/ArticleListPageInfiniteScroll.test.tsx`

- [ ] **Step 1: Replace recommendation imports and state**

Import `getDailyDigest` instead of `getRecommended`, remove the now-unused `likeArticle` and `dislikeArticle` imports, and use:

```tsx
const [briefingArticles, setBriefingArticles] = useState<Article[]>([])
const [showBriefing, setShowBriefing] = useState(() => {
  try { return localStorage.getItem('showBriefing') === 'true' } catch { return false }
})
```

Remove `recommended`, `boostedIds`, `showRecommended`, `handleBoost`, and `handleDampen`.

- [ ] **Step 2: Load the current daily briefing**

Replace `loadRecommended` with:

```tsx
const loadBriefing = async () => {
  try {
    const digest = await getDailyDigest()
    setBriefingArticles(digest.articles || [])
  } catch {
    // The briefing is supplementary; leave the panel hidden on failure.
  }
}
```

Call `loadBriefing()` from the existing mount effect in place of `loadRecommended()`.

- [ ] **Step 3: Render the briefing with accurate read state**

Replace the recommendation panel with the same `rec-panel` wrapper and header, using `briefingArticles`, `showBriefing`, the label `简报`, and the storage key `showBriefing`. For each row compute `const read = isRead(article)` and render:

```tsx
<div
  key={article.id}
  className="rec-row"
  role="link"
  tabIndex={0}
  style={{ cursor: 'pointer', opacity: read ? 0.6 : 1 }}
  onClick={() => openArticle(article.id, article)}
  onKeyDown={(event) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      openArticle(article.id, article)
    }
  }}
>
  <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
    {!read && (
      <span
        aria-label="未读"
        style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--accent)', flexShrink: 0, marginTop: 6 }}
      />
    )}
    <div style={{ flex: 1 }}>
      <div className={read ? 'text-muted' : 'text-bold'} style={{ display: 'flex', alignItems: 'center' }}>
        <MediaIndicator article={article} onPlay={player.playArticle} />
        <span>{article.title}</span>
      </div>
      <div className="flex gap-2 mt-1">
        <span className="text-muted text-sm">{formatDate(article.published_at)}</span>
        {article.feed_title && (
          <FeedSourceLink
            feedId={article.feed_id}
            label={article.feed_title}
            search={sourceSearch}
            className="text-sm"
            style={{ padding: '1px 6px', background: 'var(--accent-soft)', borderRadius: 4, color: 'var(--accent)' }}
            onNavigate={handleSourceFilter}
          />
        )}
      </div>
    </div>
  </div>
</div>
```

Keep the current visibility conditions (`!isClippingMode`, no search, no grouped view), and omit the panel when the digest contains no articles.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run:

```bash
cd frontend && npm test -- ArticleListPageInfiniteScroll.test.tsx
```

Expected: all tests in the file PASS.

- [ ] **Step 5: Commit the behavior change**

```bash
git add frontend/src/pages/ArticleListPage.tsx frontend/test/ArticleListPageInfiniteScroll.test.tsx
git commit -m "feat(frontend): replace article recommendations with briefing"
```

### Task 3: Verify the frontend

**Files:**
- Verify: `frontend/src/pages/ArticleListPage.tsx`
- Verify: `frontend/test/ArticleListPageInfiniteScroll.test.tsx`

- [ ] **Step 1: Run the full frontend checks**

```bash
cd frontend && npm run check
```

Expected: Vitest and all legacy Node tests PASS with no new warnings or errors.

- [ ] **Step 2: Run the production build**

```bash
cd frontend && npm run build
```

Expected: TypeScript and Vite production build PASS.

- [ ] **Step 3: Inspect the final diff and repository state**

```bash
git diff HEAD^ --check
git status --short
```

Expected: no whitespace errors; only the user's pre-existing untracked backup files remain outside the committed feature.
