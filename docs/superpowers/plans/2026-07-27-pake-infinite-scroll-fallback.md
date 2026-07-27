# Pake Infinite-Scroll Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the article list automatically fetch its next page in Pake's macOS WebView while preserving browser prefetching and the manual fallback button.

**Architecture:** Move infinite-scroll triggering into a focused React hook. The hook combines the existing `IntersectionObserver` fast path with a requestAnimationFrame-throttled scroll/resize geometry fallback and a synchronous in-flight guard; `ArticleListPage` continues to own pagination and API state.

**Tech Stack:** React 18, TypeScript, Vitest, Testing Library, Vite, Pake/Tauri macOS WebView

---

## File Map

- Create `frontend/src/hooks/useInfiniteScrollTrigger.ts`: Own observer setup,
  scroll/resize fallback, animation-frame throttling, request locking, and
  cleanup.
- Create `frontend/test/useInfiniteScrollTrigger.test.tsx`: Exercise the hook
  against controlled DOM geometry and observer callbacks.
- Modify `frontend/src/pages/ArticleListPage.tsx`: Replace the inline observer
  effect with the hook and make `loadMore` return its request promise.

### Task 1: Add the scroll-based fallback

**Files:**
- Create: `frontend/test/useInfiniteScrollTrigger.test.tsx`
- Create: `frontend/src/hooks/useInfiniteScrollTrigger.ts`

- [ ] **Step 1: Write the failing Pake fallback test**

Create `frontend/test/useInfiniteScrollTrigger.test.tsx` with the initial
scroll behavior:

```tsx
import { act, fireEvent, render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useRef } from 'react'
import { useInfiniteScrollTrigger } from '../src/hooks/useInfiniteScrollTrigger'

let targetTop = 10_000
let nextFrameId = 1
let animationFrames = new Map<number, FrameRequestCallback>()

function rect(top: number): DOMRect {
  return {
    x: 0,
    y: top,
    top,
    right: 100,
    bottom: top + 100,
    left: 0,
    width: 100,
    height: 100,
    toJSON: () => ({}),
  }
}

function flushAnimationFrames() {
  const queued = [...animationFrames.values()]
  animationFrames.clear()
  queued.forEach(callback => callback(0))
}

function Harness({
  enabled = true,
  refreshKey = 0,
  onLoadMore,
}: {
  enabled?: boolean
  refreshKey?: number
  onLoadMore: () => Promise<void>
}) {
  const targetRef = useRef<HTMLDivElement>(null)
  useInfiniteScrollTrigger({
    targetRef,
    enabled,
    refreshKey,
    rootMarginPx: 200,
    onLoadMore,
  })
  return <div ref={targetRef}>prefetch target</div>
}

beforeEach(() => {
  targetTop = 10_000
  nextFrameId = 1
  animationFrames = new Map()
  Object.defineProperty(window, 'innerHeight', {
    configurable: true,
    value: 800,
  })
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect')
    .mockImplementation(() => rect(targetTop))
  vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
    const id = nextFrameId++
    animationFrames.set(id, callback)
    return id
  }))
  vi.stubGlobal('cancelAnimationFrame', vi.fn((id: number) => {
    animationFrames.delete(id)
  }))
})

describe('useInfiniteScrollTrigger', () => {
  it('loads on scroll near the prefetch boundary when no observer callback arrives', async () => {
    const onLoadMore = vi.fn().mockResolvedValue(undefined)
    render(<Harness onLoadMore={onLoadMore} />)

    act(flushAnimationFrames)
    expect(onLoadMore).not.toHaveBeenCalled()

    targetTop = 900
    fireEvent.scroll(window)
    act(flushAnimationFrames)
    await act(async () => {})

    expect(onLoadMore).toHaveBeenCalledTimes(1)
  })
})
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
cd frontend
npm test -- test/useInfiniteScrollTrigger.test.tsx
```

Expected: FAIL because `../src/hooks/useInfiniteScrollTrigger` does not exist.

- [ ] **Step 3: Implement the minimal scroll fallback**

Create `frontend/src/hooks/useInfiniteScrollTrigger.ts`:

```ts
import { useEffect, type RefObject } from 'react'

interface InfiniteScrollOptions {
  targetRef: RefObject<HTMLElement>
  enabled: boolean
  refreshKey: number
  rootMarginPx?: number
  onLoadMore: () => Promise<void>
}

export function useInfiniteScrollTrigger({
  targetRef,
  enabled,
  refreshKey,
  rootMarginPx = 200,
  onLoadMore,
}: InfiniteScrollOptions) {
  useEffect(() => {
    const target = targetRef.current
    if (!enabled || !target) return

    let frameId: number | null = null
    const checkPosition = () => {
      frameId = null
      if (target.getBoundingClientRect().top <= window.innerHeight + rootMarginPx) {
        void onLoadMore()
      }
    }
    const scheduleCheck = () => {
      if (frameId === null) frameId = requestAnimationFrame(checkPosition)
    }

    window.addEventListener('scroll', scheduleCheck, { passive: true })
    document.addEventListener('scroll', scheduleCheck, {
      capture: true,
      passive: true,
    })
    window.addEventListener('resize', scheduleCheck)
    scheduleCheck()

    return () => {
      window.removeEventListener('scroll', scheduleCheck)
      document.removeEventListener('scroll', scheduleCheck, true)
      window.removeEventListener('resize', scheduleCheck)
      if (frameId !== null) cancelAnimationFrame(frameId)
    }
  }, [enabled, onLoadMore, refreshKey, rootMarginPx, targetRef])
}
```

- [ ] **Step 4: Run the focused test and verify GREEN**

Run:

```bash
cd frontend
npm test -- test/useInfiniteScrollTrigger.test.tsx
```

Expected: PASS with one test.

- [ ] **Step 5: Commit the fallback behavior**

```bash
git add frontend/src/hooks/useInfiniteScrollTrigger.ts frontend/test/useInfiniteScrollTrigger.test.tsx
git commit -m "fix: add scroll fallback for infinite loading"
```

### Task 2: Preserve observer prefetching

**Files:**
- Modify: `frontend/test/useInfiniteScrollTrigger.test.tsx`
- Modify: `frontend/src/hooks/useInfiniteScrollTrigger.ts`

- [ ] **Step 1: Add a failing observer test**

Add these declarations above `beforeEach`:

```tsx
let intersectionCallback: IntersectionObserverCallback
let intersectionObserver: TestIntersectionObserver

class TestIntersectionObserver implements IntersectionObserver {
  readonly root = null
  readonly rootMargin = '200px'
  readonly thresholds = [0]
  observe = vi.fn()
  unobserve = vi.fn()
  disconnect = vi.fn()
  takeRecords = vi.fn(() => [])

  constructor(callback: IntersectionObserverCallback) {
    intersectionCallback = callback
    intersectionObserver = this
  }
}
```

Add this setup inside `beforeEach`:

```tsx
vi.stubGlobal('IntersectionObserver', TestIntersectionObserver)
```

Add this test inside the existing `describe`:

```tsx
it('keeps IntersectionObserver as the browser prefetch path', async () => {
  const onLoadMore = vi.fn().mockResolvedValue(undefined)
  render(<Harness onLoadMore={onLoadMore} />)

  await act(async () => {
    intersectionCallback(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      intersectionObserver,
    )
  })

  expect(intersectionObserver.observe).toHaveBeenCalledTimes(1)
  expect(onLoadMore).toHaveBeenCalledTimes(1)
})
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
cd frontend
npm test -- test/useInfiniteScrollTrigger.test.tsx
```

Expected: FAIL because the hook never creates an `IntersectionObserver`, so
`intersectionCallback` is unavailable.

- [ ] **Step 3: Add the observer path to the hook**

In `useInfiniteScrollTrigger.ts`, create the observer after `scheduleCheck`:

```ts
const observer = new IntersectionObserver(
  entries => {
    if (entries.some(entry => entry.isIntersecting)) {
      void onLoadMore()
    }
  },
  { rootMargin: `${rootMarginPx}px` },
)
observer.observe(target)
```

Add observer cleanup before the animation-frame cleanup:

```ts
observer.disconnect()
```

- [ ] **Step 4: Run the focused test and verify GREEN**

Run:

```bash
cd frontend
npm test -- test/useInfiniteScrollTrigger.test.tsx
```

Expected: PASS with two tests.

- [ ] **Step 5: Commit observer preservation**

```bash
git add frontend/src/hooks/useInfiniteScrollTrigger.ts frontend/test/useInfiniteScrollTrigger.test.tsx
git commit -m "test: preserve observer infinite-scroll path"
```

### Task 3: Guard duplicate loads and make failures retryable

**Files:**
- Modify: `frontend/test/useInfiniteScrollTrigger.test.tsx`
- Modify: `frontend/src/hooks/useInfiniteScrollTrigger.ts`

- [ ] **Step 1: Add failing concurrency and retry tests**

Add these tests inside the existing `describe`:

```tsx
it('starts only one load when observer and scroll fire together', async () => {
  let finishLoad!: () => void
  const pendingLoad = new Promise<void>(resolve => {
    finishLoad = resolve
  })
  const onLoadMore = vi.fn(() => pendingLoad)
  render(<Harness onLoadMore={onLoadMore} />)

  targetTop = 900
  await act(async () => {
    intersectionCallback(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      intersectionObserver,
    )
    fireEvent.scroll(window)
    flushAnimationFrames()
    await Promise.resolve()
  })

  expect(onLoadMore).toHaveBeenCalledTimes(1)

  await act(async () => {
    finishLoad()
    await pendingLoad
  })
})

it('releases the guard after failure so a later scroll can retry', async () => {
  const onLoadMore = vi.fn()
    .mockRejectedValueOnce(new Error('network unavailable'))
    .mockResolvedValueOnce(undefined)
  render(<Harness onLoadMore={onLoadMore} />)
  targetTop = 900

  fireEvent.scroll(window)
  act(flushAnimationFrames)
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })

  fireEvent.scroll(window)
  act(flushAnimationFrames)
  await act(async () => {
    await Promise.resolve()
  })

  expect(onLoadMore).toHaveBeenCalledTimes(2)
})

it('does not load when disabled', () => {
  const onLoadMore = vi.fn().mockResolvedValue(undefined)
  render(<Harness enabled={false} onLoadMore={onLoadMore} />)

  targetTop = 900
  fireEvent.scroll(window)
  act(flushAnimationFrames)
  expect(onLoadMore).not.toHaveBeenCalled()
})

it('disconnects the observer and cancels queued work on unmount', () => {
  const onLoadMore = vi.fn().mockResolvedValue(undefined)
  const { unmount } = render(<Harness onLoadMore={onLoadMore} />)

  unmount()
  fireEvent.scroll(window)
  expect(intersectionObserver.disconnect).toHaveBeenCalledTimes(1)
  expect(animationFrames.size).toBe(0)
})
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
cd frontend
npm test -- test/useInfiniteScrollTrigger.test.tsx
```

Expected: FAIL because the observer and scroll paths can call `onLoadMore`
twice, and a rejected promise is not absorbed by the hook.

- [ ] **Step 3: Replace direct calls with one guarded trigger**

Update the import:

```ts
import { useCallback, useEffect, useRef, type RefObject } from 'react'
```

Add the guard before the effect:

```ts
const inFlightRef = useRef(false)
const triggerLoad = useCallback(() => {
  if (!enabled || inFlightRef.current) return
  inFlightRef.current = true
  void Promise.resolve()
    .then(onLoadMore)
    .catch(() => {})
    .finally(() => {
      inFlightRef.current = false
    })
}, [enabled, onLoadMore])
```

Replace both `void onLoadMore()` calls with:

```ts
triggerLoad()
```

Replace `onLoadMore` with `triggerLoad` in the effect dependency list:

```ts
}, [enabled, refreshKey, rootMarginPx, targetRef, triggerLoad])
```

- [ ] **Step 4: Run the focused test and verify GREEN**

Run:

```bash
cd frontend
npm test -- test/useInfiniteScrollTrigger.test.tsx
```

Expected: PASS with six tests and no unhandled rejection.

- [ ] **Step 5: Commit request guarding**

```bash
git add frontend/src/hooks/useInfiniteScrollTrigger.ts frontend/test/useInfiniteScrollTrigger.test.tsx
git commit -m "fix: guard infinite-scroll requests"
```

### Task 4: Integrate the hook into the article list

**Files:**
- Modify: `frontend/src/pages/ArticleListPage.tsx:1-22`
- Modify: `frontend/src/pages/ArticleListPage.tsx:398-418`

- [ ] **Step 1: Confirm the old page still owns the observer**

Run:

```bash
rg -n "new IntersectionObserver|useInfiniteScrollTrigger" frontend/src/pages/ArticleListPage.tsx
```

Expected: one `new IntersectionObserver` match and no hook match.

- [ ] **Step 2: Import and call the hook**

Add the import:

```ts
import { useInfiniteScrollTrigger } from '../hooks/useInfiniteScrollTrigger'
```

Make `loadMore` return the asynchronous request:

```ts
const loadMore = useCallback(async () => {
  if (!loadingMore && hasMore) {
    await loadArticles(offset + PAGE_SIZE, false)
  }
}, [loadingMore, hasMore, offset, loadArticles])
```

Delete the inline `IntersectionObserver` effect and replace it with:

```ts
useInfiniteScrollTrigger({
  targetRef: loadMoreRef,
  enabled: hasMore
    && !loadingMore
    && searchQuery.length === 0
    && !grouped
    && !isClippingMode,
  refreshKey: articles.length,
  rootMarginPx: 200,
  onLoadMore: loadMore,
})
```

- [ ] **Step 3: Run hook tests and the TypeScript production build**

Run:

```bash
cd frontend
npm test -- test/useInfiniteScrollTrigger.test.tsx
npm run build
```

Expected: all hook tests PASS; `tsc && vite build` exits 0.

- [ ] **Step 4: Confirm the page has one shared infinite-scroll trigger**

Run:

```bash
rg -n "new IntersectionObserver|useInfiniteScrollTrigger" frontend/src/pages/ArticleListPage.tsx frontend/src/hooks/useInfiniteScrollTrigger.ts
```

Expected: `ArticleListPage.tsx` calls `useInfiniteScrollTrigger`; only the hook
file constructs `IntersectionObserver`.

- [ ] **Step 5: Commit page integration**

```bash
git add frontend/src/pages/ArticleListPage.tsx
git commit -m "fix: use compatible article infinite scroll"
```

### Task 5: Verify, publish, deploy, and reproduce in Pake

**Files:**
- Verify only; no planned source changes

- [ ] **Step 1: Run the complete frontend verification**

Run:

```bash
cd frontend
npm run check
npm run build
```

Expected: Vitest tests PASS, legacy Node tests PASS, and the Vite production
build exits 0 without TypeScript errors.

- [ ] **Step 2: Review the exact branch diff**

Run:

```bash
git diff master...HEAD --check
git diff master...HEAD --stat
git status --short
```

Expected: only the hook, hook test, and article-list integration are code
changes; no backup files are staged or modified.

- [ ] **Step 3: Fast-forward master and publish**

From `/Users/bytedance/mygit/rss-pal`:

```bash
git merge --ff-only fix/pake-infinite-scroll-fallback
git push origin master
```

Expected: local `master` and `origin/master` point to the verified
implementation commit.

- [ ] **Step 4: Deploy the published master to OCI**

Run:

```bash
ssh oci-rss-pal 'cd /opt/rss-pal && ./scripts/auto_deploy.sh'
curl -fsS https://rss.morefreeze.top/api/health
```

Expected: deploy output reports success on the published commit and the health
endpoint returns HTTP 200.

- [ ] **Step 5: Verify the real installed Pake application**

Use the `computer-use` skill to open `/Applications/rsspal.app`, reload the
article list, and scroll from above the prefetch card toward the bottom.

Expected:

- `加载中...` appears before the manual button needs to be clicked.
- Older articles are appended beyond the original 20 rows.
- Reaching the next prefetch boundary appends another page only once.
- The `加载更多` button remains available whenever no automatic request is in
  flight.
