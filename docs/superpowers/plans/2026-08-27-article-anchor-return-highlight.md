# Article Anchor Return Highlight Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show valid summary references as `跳转原文 ⌖` and highlight the exact summary source for the same seven-second interval after the reader returns with `↩⌖`.

**Architecture:** Keep scroll and return-link lifecycle ownership in `articleAnchorRoundTrip.ts`, adding one optional callback that fires only for a valid return. Keep highlight timing in `SummaryMarkdown.tsx`; the callback reuses `restartArticleHighlight` so forward and return highlights share one implementation and fallback duration.

**Tech Stack:** React 18, TypeScript, ReactMarkdown, Vitest, Testing Library, CSS.

---

### Task 1: Add a valid-return callback to the round-trip controller

**Files:**
- Modify: `frontend/src/util/articleAnchorRoundTrip.ts`
- Test: `frontend/src/util/articleAnchorRoundTrip.test.ts`

- [ ] **Step 1: Write the failing controller tests**

Extend the successful-return test with an `onReturn` spy and add a detached-source assertion:

```ts
const onReturn = vi.fn()
startArticleAnchorRoundTrip(source, target, { onReturn })
fireEvent.click(back!, { detail: 1 })
expect(onReturn).toHaveBeenCalledTimes(1)
```

In `cleans up safely when the recorded source is detached`, pass the same option and assert:

```ts
expect(onReturn).not.toHaveBeenCalled()
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `cd frontend && npm test -- --run src/util/articleAnchorRoundTrip.test.ts`

Expected: FAIL because `startArticleAnchorRoundTrip` accepts two arguments and never calls `onReturn`.

- [ ] **Step 3: Implement the minimal callback contract**

Add the option type and optional parameter:

```ts
type ArticleAnchorRoundTripOptions = {
  onReturn?: () => void
}

export function startArticleAnchorRoundTrip(
  source: HTMLAnchorElement,
  target: HTMLElement,
  options: ArticleAnchorRoundTripOptions = {},
): void {
```

In the plain-primary return handler, after scroll/focus succeeds and before cleanup, invoke:

```ts
options.onReturn?.()
clearArticleAnchorRoundTrip(source)
```

Do not invoke the callback for modified clicks, auxiliary clicks, or detached sources.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run: `cd frontend && npm test -- --run src/util/articleAnchorRoundTrip.test.ts`

Expected: 8 tests pass.

- [ ] **Step 5: Commit the controller change**

```bash
git add frontend/src/util/articleAnchorRoundTrip.ts frontend/src/util/articleAnchorRoundTrip.test.ts
git commit -m "feat(frontend): expose article anchor return callback"
```

### Task 2: Restore the forward label and highlight the returned source

**Files:**
- Modify: `frontend/src/components/SummaryMarkdown.tsx`
- Test: `frontend/src/components/SummaryMarkdown.test.tsx`
- Modify: `frontend/src/index.css`
- Test: `frontend/test/articleAnchorRoundTripStyles.test.ts`

- [ ] **Step 1: Write failing rendering and return-highlight tests**

Change the normalized article-link rendering expectation to:

```ts
expect(articleLink.textContent).toBe('跳转原文 ⌖')
```

In the round-trip test, enable fake timers before activation and add:

```ts
expect(source.classList.contains('article-anchor-highlight')).toBe(true)
act(() => vi.advanceTimersByTime(6_999))
expect(source.classList.contains('article-anchor-highlight')).toBe(true)
act(() => vi.advanceTimersByTime(101))
expect(source.classList.contains('article-anchor-highlight')).toBe(false)
```

Extend the style test to require content-sized inline layout:

```ts
const summaryRule = css.match(/\.summary-article-link\s*\{[^}]+\}/)?.[0] ?? ''
expect(summaryRule).toMatch(/display:\s*inline-flex/)
expect(summaryRule).not.toMatch(/\bwidth\s*:/)
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `cd frontend && npm test -- --run src/components/SummaryMarkdown.test.tsx test/articleAnchorRoundTripStyles.test.ts`

Expected: FAIL because the forward link is icon-only, returning does not highlight the source, and the CSS still fixes the link width.

- [ ] **Step 3: Render the visible label and icon**

Replace the valid-target children with:

```tsx
{targetID ? (
  <>
    <span className="summary-article-link-label">跳转原文</span>
    <span className="summary-article-link-icon" aria-hidden="true">⌖</span>
  </>
) : children}
```

Keep the existing `aria-label="跳转原文"`, `title`, strict `href`, and source ID behavior.

- [ ] **Step 4: Reuse the highlight timer on return**

Capture the current source before starting the trip and pass:

```ts
const source = sourceRef.current
if (source) {
  startArticleAnchorRoundTrip(source, target, {
    onReturn: () => restartArticleHighlight(source),
  })
}
```

The callback must reuse `restartArticleHighlight`; do not add another timeout constant.

- [ ] **Step 5: Make the forward control content-sized**

Update `.summary-article-link` to use `display: inline-flex`, `align-items: center`, a small `gap`, `min-height: 1.5em`, and horizontal padding. Remove the fixed `width` and `height` declarations while retaining border, hover/focus, background, and inline alignment styles.

- [ ] **Step 6: Run the focused tests and verify GREEN**

Run: `cd frontend && npm test -- --run src/components/SummaryMarkdown.test.tsx test/articleAnchorRoundTripStyles.test.ts`

Expected: both test files pass.

- [ ] **Step 7: Commit the UI behavior**

```bash
git add frontend/src/components/SummaryMarkdown.tsx frontend/src/components/SummaryMarkdown.test.tsx frontend/src/index.css frontend/test/articleAnchorRoundTripStyles.test.ts
git commit -m "feat(frontend): highlight returned summary anchor"
```

### Task 3: Verify the complete frontend result

**Files:**
- Verify only; no planned source changes.

- [ ] **Step 1: Run the complete frontend test suite**

Run: `cd frontend && npm test -- --run`

Expected: all test files and tests pass.

- [ ] **Step 2: Build the production frontend**

Run: `cd frontend && npm run build`

Expected: TypeScript and Vite build succeed; the existing chunk-size warning is acceptable.

- [ ] **Step 3: Check patch hygiene and branch state**

Run: `git diff --check master...HEAD && git status --short`

Expected: no whitespace errors and no uncommitted planned changes.

- [ ] **Step 4: Report exact evidence and route branch completion**

Report focused/full test counts, build output, commits, and changed files. Then use `finishing-a-development-branch`; Option 1 merges into the resolved base branch and pushes it to `origin` only after merged-result tests pass.
