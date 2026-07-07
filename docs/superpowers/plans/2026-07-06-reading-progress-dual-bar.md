# Reading Progress Historical Bar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render a single light-blue article progress bar that represents only the persisted historical high-water reading position.

**Architecture:** Keep the backend progress API unchanged. Use pure helpers for scroll-progress math and stable display derivation, use them from `ArticlePage`, and keep persistence monotonic by writing only when current progress exceeds the high-water mark. Render the top bar through a dedicated `ArticleProgressBar` component that only accepts the saved historical percent.

**Follow-up implementation note:** A later bug report showed the historical
layer still jumped because the first implementation retained layout-height
rescaling. The final implementation removes `rescaleProgressForHeightChange`
entirely: content height changes only re-measure the current viewport layer;
the historical layer changes only when the user scrolls beyond the saved
high-water mark or explicitly marks read/unread.

**Current implementation note:** The top progress UI is a single light-blue
historical high-water bar. The dark-blue current-position layer and old
AI-marker/confetti visual path are not mounted. `frontend/test/articleHistoricalProgressBar.test.cjs`
guards this state.

**Tech Stack:** React 18, TypeScript, Vite, no additional runtime dependencies.

---

## File Structure

- Create `frontend/src/utils/readingProgress.ts`: pure progress helpers for clamping, viewport progress, high-water updates, and stable display derivation.
- Create `frontend/test/readingProgress.test.ts`: no-dependency TypeScript assertions for the helper.
- Modify `frontend/src/pages/ArticlePage.tsx`: maintain `currentScrollPosition` separately from `progress.scroll_position`; wire helper into scroll, restore, mark-read, mark-unread, and resize paths.
- Create `frontend/src/components/ArticleProgressBar.tsx`: isolated progress-bar rendering for the historical high-water fill.
- Modify `frontend/src/index.css`: add progress-track and single light-blue historical fill classes.
- Add `frontend/test/articleHistoricalProgressBar.test.cjs` to ensure the article page mounts only the historical bar and does not pass current viewport progress into it.

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

In the ResizeObserver callback, re-measure only the local current viewport
metadata. Do not rescale `maxScrollRef.current` or `progress.scroll_position`:

```ts
setCurrentScrollPosition(computeViewportProgress(window.scrollY, newHeight, vh))
```

- [ ] **Step 5: Render the historical progress layer**

Render the fixed progress bar through `ArticleProgressBar`:

```tsx
<ArticleProgressBar
  historicalPercent={progressDisplay.historicalPercent}
/>
```

Use `deriveProgressDisplay` so the top bar reads only from the saved
high-water mark:

```ts
const progressDisplay = deriveProgressDisplay({
  currentPosition: currentScrollPosition,
  historicalHighWater: progress?.scroll_position ?? maxScrollRef.current,
})
```

The metadata text can keep using `progressDisplay.currentPercent`.

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
  background: #93c5fd;
}
```

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
gh pr create --title "fix: show saved reading progress" --body "$(cat <<'EOF'
## Summary
- render a single light-blue top bar for saved historical high-water progress
- keep progress persistence monotonic and server-backed
- remove the dark-blue current progress layer from the top bar

## Test Plan
- npm run build
- focused TypeScript helper test for reading-progress math
- local Vite deployment started
EOF
)"
```

Expected: branch push succeeds and `gh` prints the PR URL.
