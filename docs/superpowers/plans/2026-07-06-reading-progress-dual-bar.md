# Reading Progress Dual Bar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split article-page reading progress into a persisted historical high-water mark and a local current viewport position, then render them as light-blue and dark-blue progress layers.

**Architecture:** Keep the backend progress API unchanged. Add a small pure helper for scroll-progress math, use it from `ArticlePage`, and keep persistence monotonic by writing only when current progress exceeds the high-water mark. Style the existing fixed top progress bar as two overlapping fills.

**Tech Stack:** React 18, TypeScript, Vite, no additional runtime dependencies.

---

## File Structure

- Create `frontend/src/utils/readingProgress.ts`: pure progress helpers for clamping, viewport progress, high-water updates, and height-change rescaling.
- Create `frontend/test/readingProgress.test.ts`: no-dependency TypeScript assertions for the helper.
- Modify `frontend/src/pages/ArticlePage.tsx`: maintain `currentScrollPosition` separately from `progress.scroll_position`; wire helper into scroll, restore, mark-read, mark-unread, and resize paths.
- Modify `frontend/src/index.css`: add progress-track and two-fill classes.

## Task 1: Progress Math Test

**Files:**
- Create: `frontend/test/readingProgress.test.ts`
- Create later: `frontend/src/utils/readingProgress.ts`

- [ ] **Step 1: Write the failing test**

Create `frontend/test/readingProgress.test.ts`:

```ts
import {
  computeViewportProgress,
  evaluateReadingProgress,
  rescaleProgressForHeightChange,
} from '../src/utils/readingProgress'

function assertEqual<T>(actual: T, expected: T, label: string) {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${String(expected)}, got ${String(actual)}`)
  }
}

function assertClose(actual: number, expected: number, label: string) {
  if (Math.abs(actual - expected) > 0.000001) {
    throw new Error(`${label}: expected ${expected}, got ${actual}`)
  }
}

assertClose(computeViewportProgress(250, 1500, 500), 0.25, 'viewport progress')
assertClose(computeViewportProgress(-50, 1500, 500), 0, 'viewport clamps low')
assertClose(computeViewportProgress(2000, 1500, 500), 1, 'viewport clamps high')

const belowSaved = evaluateReadingProgress({
  currentPosition: 0.2,
  savedHighWater: 0.6,
  activeReadSeconds: 0,
  readingMinutes: 4,
})
assertClose(belowSaved.currentPosition, 0.2, 'below saved current')
assertClose(belowSaved.highWaterPosition, 0.6, 'below saved high-water')
assertEqual(belowSaved.shouldPersist, false, 'below saved does not persist')
assertEqual(belowSaved.isCompleted, false, 'below saved not completed')

const beyondSaved = evaluateReadingProgress({
  currentPosition: 0.72,
  savedHighWater: 0.6,
  activeReadSeconds: 0,
  readingMinutes: 4,
})
assertClose(beyondSaved.currentPosition, 0.72, 'beyond saved current')
assertClose(beyondSaved.highWaterPosition, 0.72, 'beyond saved high-water')
assertEqual(beyondSaved.shouldPersist, true, 'beyond saved persists')

const bottom = evaluateReadingProgress({
  currentPosition: 0.96,
  savedHighWater: 0.7,
  activeReadSeconds: 0,
  readingMinutes: 10,
})
assertEqual(bottom.isCompleted, true, 'bottom scroll completes')

const gated = evaluateReadingProgress({
  currentPosition: 0.91,
  savedHighWater: 0.7,
  activeReadSeconds: 15,
  readingMinutes: 10,
})
assertEqual(gated.isCompleted, true, 'time gate completes')

assertClose(
  rescaleProgressForHeightChange(0.5, 2000, 3000, 1000),
  0.25,
  'height change rescales by scrollable denominator',
)

console.log('readingProgress tests passed')
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd frontend
rm -rf /tmp/rss-pal-reading-progress-test
./node_modules/.bin/tsc --module commonjs --target ES2020 --moduleResolution node --skipLibCheck --outDir /tmp/rss-pal-reading-progress-test test/readingProgress.test.ts src/utils/readingProgress.ts
node /tmp/rss-pal-reading-progress-test/test/readingProgress.test.js
```

Expected: TypeScript fails because `src/utils/readingProgress.ts` does not exist yet.

## Task 2: Implement Progress Math Helper

**Files:**
- Create: `frontend/src/utils/readingProgress.ts`
- Test: `frontend/test/readingProgress.test.ts`

- [ ] **Step 1: Add minimal helper implementation**

Create `frontend/src/utils/readingProgress.ts`:

```ts
export interface ReadingProgressInput {
  currentPosition: number
  savedHighWater: number
  activeReadSeconds: number
  readingMinutes?: number
}

export interface ReadingProgressResult {
  currentPosition: number
  highWaterPosition: number
  shouldPersist: boolean
  isCompleted: boolean
}

export function clampProgress(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.min(1, Math.max(0, value))
}

export function computeViewportProgress(
  scrollTop: number,
  contentScrollHeight: number,
  viewportHeight: number,
): number {
  const scrollableHeight = contentScrollHeight - viewportHeight
  if (scrollableHeight <= 0) return 0
  return clampProgress(scrollTop / scrollableHeight)
}

export function evaluateReadingProgress(input: ReadingProgressInput): ReadingProgressResult {
  const currentPosition = clampProgress(input.currentPosition)
  const savedHighWater = clampProgress(input.savedHighWater)
  const highWaterPosition = Math.max(savedHighWater, currentPosition)
  const readMinutes = input.readingMinutes && input.readingMinutes > 0 ? input.readingMinutes : 1
  const minSeconds = Math.min(15, Math.floor(readMinutes * 30))
  const isCompleted = currentPosition > 0.95 ||
    (currentPosition > 0.9 && input.activeReadSeconds >= minSeconds)

  return {
    currentPosition,
    highWaterPosition,
    shouldPersist: currentPosition > savedHighWater,
    isCompleted,
  }
}

export function rescaleProgressForHeightChange(
  progress: number,
  oldContentHeight: number,
  newContentHeight: number,
  viewportHeight: number,
): number {
  const oldScrollable = oldContentHeight - viewportHeight
  const newScrollable = newContentHeight - viewportHeight
  if (oldScrollable < 1 || newScrollable < 1) return clampProgress(progress)
  return clampProgress(progress * (oldScrollable / newScrollable))
}
```

- [ ] **Step 2: Run test to verify it passes**

Run the same command from Task 1 Step 2.

Expected: command prints `readingProgress tests passed`.

## Task 3: Wire Dual Progress Into Article Page

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`
- Modify: `frontend/src/index.css`
- Test: `frontend/test/readingProgress.test.ts`

- [ ] **Step 1: Import helpers and add current state**

In `frontend/src/pages/ArticlePage.tsx`, import:

```ts
import {
  computeViewportProgress,
  evaluateReadingProgress,
  rescaleProgressForHeightChange,
} from '../utils/readingProgress'
```

Add state next to existing `progress`:

```ts
const [currentScrollPosition, setCurrentScrollPosition] = useState(0)
```

- [ ] **Step 2: Initialize and reset current progress with article lifecycle**

When article data loads, set both saved high-water and current display from the saved value:

```ts
const savedScrollPosition = Math.min(1, data.progress?.scroll_position ?? 0)
setProgress(data.progress)
maxScrollRef.current = savedScrollPosition
setCurrentScrollPosition(savedScrollPosition)
```

When `id` changes, reset before loading:

```ts
setCurrentScrollPosition(0)
```

- [ ] **Step 3: Keep current display in sync with scroll restore and scroll events**

After a successful restore scroll, add:

```ts
setCurrentScrollPosition(saved)
```

At the start of `handleScroll`, compute and store current progress even when it is below the high-water mark:

```ts
const scrollPosition = computeViewportProgress(scrollTop, contentRef.current.scrollHeight, window.innerHeight)
const nextReadingProgress = evaluateReadingProgress({
  currentPosition: scrollPosition,
  savedHighWater: maxScrollRef.current,
  activeReadSeconds: activeReadSecondsRef.current,
  readingMinutes: article.reading_minutes,
})
setCurrentScrollPosition(nextReadingProgress.currentPosition)

if (!nextReadingProgress.shouldPersist) return
maxScrollRef.current = nextReadingProgress.highWaterPosition
```

Use `nextReadingProgress.isCompleted` and `nextReadingProgress.highWaterPosition` for the existing persistence state.

- [ ] **Step 4: Update mark-read, mark-unread, and ResizeObserver paths**

In mark-read:

```ts
setCurrentScrollPosition(1)
```

In mark-unread:

```ts
setCurrentScrollPosition(0)
```

In the ResizeObserver callback, replace the manual rescale factor with:

```ts
const nextHighWater = rescaleProgressForHeightChange(maxScrollRef.current, lastHeight, newHeight, vh)
maxScrollRef.current = nextHighWater
setCurrentScrollPosition(computeViewportProgress(window.scrollY, newHeight, vh))
setProgress((prev) => prev ? { ...prev, scroll_position: nextHighWater } : prev)
```

- [ ] **Step 5: Render two progress layers**

Replace the inline fixed progress bar with a classed track:

```tsx
<div className="article-progress-track">
  <div
    className="article-progress-fill article-progress-fill-history"
    style={{ width: `${historicalProgressPercent}%` }}
  />
  <div
    className="article-progress-fill article-progress-fill-current"
    style={{ width: `${currentProgressPercent}%` }}
  />
  {aiMarkerPos !== null && (...)}
</div>
```

Use:

```ts
const historicalProgressPercent = progress?.scroll_position ? Math.min(100, Math.round(progress.scroll_position * 100)) : 0
const currentProgressPercent = currentScrollPosition ? Math.min(100, Math.round(currentScrollPosition * 100)) : 0
```

The metadata text should read `currentProgressPercent`.

- [ ] **Step 6: Add CSS classes**

Add to `frontend/src/index.css`:

```css
.article-progress-track {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 4px;
  background: var(--border);
  z-index: 1000;
}

.article-progress-fill {
  position: absolute;
  left: 0;
  top: 0;
  height: 100%;
  transition: width 0.3s ease;
}

.article-progress-fill-history {
  z-index: 1;
  background: #93c5fd;
}

.article-progress-fill-current {
  z-index: 2;
  background: #0066cc;
}
```

Update `.ai-marker` with `z-index: 3;`.

- [ ] **Step 7: Run focused test and full build**

Run:

```bash
cd frontend
rm -rf /tmp/rss-pal-reading-progress-test
./node_modules/.bin/tsc --module commonjs --target ES2020 --moduleResolution node --skipLibCheck --outDir /tmp/rss-pal-reading-progress-test test/readingProgress.test.ts src/utils/readingProgress.ts
node /tmp/rss-pal-reading-progress-test/test/readingProgress.test.js
npm run build
```

Expected: helper test prints `readingProgress tests passed`; build exits 0.

## Task 4: Local Deploy And PR

**Files:**
- No source file changes expected.

- [ ] **Step 1: Start local frontend dev server**

Run:

```bash
cd frontend
npm run dev -- --host 127.0.0.1
```

Expected: Vite prints a local URL, normally `http://127.0.0.1:5173/`.

- [ ] **Step 2: Commit implementation**

Run:

```bash
git status --short
git add frontend/src/utils/readingProgress.ts frontend/test/readingProgress.test.ts frontend/src/pages/ArticlePage.tsx frontend/src/index.css docs/superpowers/plans/2026-07-06-reading-progress-dual-bar.md
git commit -m "fix: show current and saved reading progress"
```

- [ ] **Step 3: Push and create PR**

Run:

```bash
git push -u origin feat/reading-progress-dual-bar
gh pr create --title "fix: show current and saved reading progress" --body "$(cat <<'EOF'
## Summary
- split article progress display into historical high-water and current viewport progress
- render light-blue saved progress behind dark-blue current progress
- keep progress persistence monotonic with focused helper coverage

## Test Plan
- npm run build
- focused TypeScript helper test for reading-progress math
- local Vite deployment started
EOF
)"
```

Expected: branch push succeeds and `gh` prints the PR URL.
