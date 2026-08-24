# Summary Anchor Highlight and Link Icon Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep article-anchor targets highlighted for seven seconds and make each valid `查看原文` link easier to notice with a decorative upper-right crosshair.

**Architecture:** Extend the existing `SummaryLink` renderer only when `parseArticleAnchor` accepts the fragment. The link remains the sole interactive element; CSS presents an `aria-hidden` crosshair and owns the seven-second fade, while the existing JavaScript cleanup remains a fallback and protects repeated activations.

**Tech Stack:** React 18, TypeScript, React Markdown, Vitest, Testing Library, CSS, Vite

---

## File structure

- Modify `frontend/src/components/SummaryMarkdown.tsx`: render the decorative icon for valid article links and extend fallback cleanup to 7.1 seconds.
- Modify `frontend/src/components/SummaryMarkdown.test.tsx`: specify valid-link icon behavior, ordinary-link isolation, seven-second timing, repeat-click timing, and reduced-motion cleanup.
- Modify `frontend/src/index.css`: style the link/icon and change the highlight animation to seven seconds.

### Task 1: Specify the icon and seven-second lifecycle

**Files:**
- Test: `frontend/src/components/SummaryMarkdown.test.tsx`

- [ ] **Step 1: Update the shared test timeout and add failing icon assertions**

Change the test constant and add this test inside `describe('SummaryMarkdown article links', ...)`:

```tsx
const HIGHLIGHT_TIMEOUT_MS = 7_100

it('adds one decorative crosshair to a valid article link only', () => {
  render(<SummaryMarkdown source={'[查看原文](#article-section-001)\n\n[External](https://example.com)\n\n[Invalid](#article-section-01)'} />)

  const articleLink = screen.getByRole('link', { name: '查看原文' })
  const icon = articleLink.querySelector('.summary-article-link-icon')
  expect(articleLink).toHaveClass('summary-article-link')
  expect(icon).toHaveTextContent('⌖')
  expect(icon).toHaveAttribute('aria-hidden', 'true')

  const externalLink = screen.getByRole('link', { name: 'External' })
  expect(externalLink).not.toHaveClass('summary-article-link')
  expect(externalLink.querySelector('.summary-article-link-icon')).toBeNull()

  const invalidLink = screen.getByRole('link', { name: 'Invalid' })
  expect(invalidLink).not.toHaveClass('summary-article-link')
  expect(invalidLink.querySelector('.summary-article-link-icon')).toBeNull()
})
```

- [ ] **Step 2: Replace the cleanup assertion with an exact seven-second boundary test**

Add this test; keep the existing explicit `animationEnd` test because it covers the normal browser cleanup path:

```tsx
it('keeps the target highlighted for seven seconds before fallback cleanup', () => {
  vi.useFakeTimers()
  const target = addArticleTarget()
  render(<SummaryMarkdown source="[Jump](#article-section-001)" />)

  fireEvent.click(screen.getByRole('link', { name: 'Jump' }), { detail: 1 })
  act(() => vi.advanceTimersByTime(6_999))
  expect(target.classList.contains('article-anchor-highlight')).toBe(true)

  act(() => vi.advanceTimersByTime(101))
  expect(target.classList.contains('article-anchor-highlight')).toBe(false)
})
```

- [ ] **Step 3: Extend the reduced-motion test with fallback cleanup**

Turn on fake timers at the start of the existing reduced-motion test and append:

```tsx
act(() => vi.advanceTimersByTime(7_000))
expect(target.classList.contains('article-anchor-highlight')).toBe(true)
act(() => vi.advanceTimersByTime(100))
expect(target.classList.contains('article-anchor-highlight')).toBe(false)
```

The existing repeat-click test already uses `HIGHLIGHT_TIMEOUT_MS`; changing the shared test constant to `7_100` makes it verify that an older 7.1-second timer cannot clear a newer highlight.

- [ ] **Step 4: Run the focused tests and verify RED**

Run from `frontend/`:

```bash
npm test -- --run src/components/SummaryMarkdown.test.tsx
```

Expected: FAIL because the valid article link has no icon/classes and fallback cleanup still runs at 1.3 seconds.

- [ ] **Step 5: Commit the failing tests**

```bash
git add frontend/src/components/SummaryMarkdown.test.tsx
git commit -m "test(reader): specify anchor highlight polish"
```

### Task 2: Render the crosshair and extend highlight timing

**Files:**
- Modify: `frontend/src/components/SummaryMarkdown.tsx`
- Modify: `frontend/src/index.css`
- Test: `frontend/src/components/SummaryMarkdown.test.tsx`

- [ ] **Step 1: Extend the fallback timeout**

In `SummaryMarkdown.tsx`, change:

```tsx
const ARTICLE_HIGHLIGHT_TIMEOUT_MS = 7_100
```

Keep `animationend` cleanup and the `activeHighlights` ownership check unchanged.

- [ ] **Step 2: Render one decorative crosshair inside valid article links**

Replace the final return in `SummaryLink` with:

```tsx
  const className = [rest.className, targetID ? 'summary-article-link' : '']
    .filter(Boolean)
    .join(' ') || undefined

  return (
    <a href={href} onClick={handleClick} onAuxClick={handleAuxClick} {...rest} className={className}>
      {children}
      {targetID && (
        <span className="summary-article-link-icon" aria-hidden="true">⌖</span>
      )}
    </a>
  )
```

Spreading `rest` before the explicit `className` preserves every existing Markdown attribute while allowing the valid-link class to be merged with any supplied class.

- [ ] **Step 3: Style the link and icon, then extend the CSS animation**

Add near the existing `.summary-markdown` rules in `frontend/src/index.css`:

```css
.summary-article-link {
  display: inline-flex;
  align-items: flex-start;
  gap: 3px;
  min-height: 28px;
  padding: 3px 4px;
}

.summary-article-link-icon {
  display: inline-grid;
  place-items: center;
  width: 1.2em;
  height: 1.2em;
  margin-top: -0.3em;
  border: 1px solid currentColor;
  border-radius: 4px;
  font-size: 0.72em;
  line-height: 1;
}
```

Change the existing animation declaration to:

```css
animation: article-anchor-highlight 7s ease-out;
```

Keep the existing `0%, 35%` keyframe plateau. At seven seconds it remains fully visible for 2.45 seconds and then fades out.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run from `frontend/`:

```bash
npm test -- --run src/components/SummaryMarkdown.test.tsx
```

Expected: all `SummaryMarkdown` tests PASS.

- [ ] **Step 5: Run related anchor tests**

Run from `frontend/`:

```bash
npm test -- --run src/components/SummaryMarkdown.test.tsx src/util/articleAnchors.test.ts
```

Expected: both test files PASS; strict anchor parsing and Markdown block mapping remain unchanged.

- [ ] **Step 6: Run the frontend build**

Run from `frontend/`:

```bash
npm run build
```

Expected: TypeScript and Vite build complete successfully.

- [ ] **Step 7: Inspect the focused diff**

```bash
git diff --check
git diff HEAD~1 -- frontend/src/components/SummaryMarkdown.tsx frontend/src/components/SummaryMarkdown.test.tsx frontend/src/index.css
```

Expected: no whitespace errors; only the approved link presentation, timing, and tests changed.

- [ ] **Step 8: Commit the implementation**

```bash
git add frontend/src/components/SummaryMarkdown.tsx frontend/src/index.css
git commit -m "feat(reader): strengthen summary anchor feedback"
```

### Task 3: Final regression verification

**Files:**
- Verify: `frontend/src/components/SummaryMarkdown.tsx`
- Verify: `frontend/src/components/SummaryMarkdown.test.tsx`
- Verify: `frontend/src/index.css`

- [ ] **Step 1: Run the complete frontend test suite**

Run from `frontend/`:

```bash
npm test -- --run
```

Expected: all frontend tests PASS with zero failed test files.

- [ ] **Step 2: Rebuild from the final committed tree**

```bash
npm run build
```

Expected: exit code 0 and a fresh `dist/` bundle.

- [ ] **Step 3: Verify repository state and commit range**

```bash
git status --short
git log --oneline origin/master..HEAD
git diff --check origin/master..HEAD
```

Expected: the worktree is clean; the range contains the design, test, and implementation commits; the diff has no whitespace errors.

- [ ] **Step 4: Request code review before integration**

Review `origin/master..HEAD` against `docs/superpowers/specs/2026-08-24-summary-anchor-highlight-icon-design.md`. Fix all Critical and Important findings, rerun the focused tests plus build, and commit any corrections before requesting production integration.
