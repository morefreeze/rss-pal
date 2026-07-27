# Article Detail Prefetch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make article content appear immediately after list navigation whenever a bounded prefetch or session-memory entry exists, while revalidating private state on every entry.

**Architecture:** A dedicated frontend cache owns article-detail requests, coalesces matching in-flight calls, and retains 30 LRU entries. The list schedules six idle prefetches with concurrency two and promotes pointer/focus/touch targets; the article page seeds from cache or a lean route preview, then applies a mandatory fresh response guarded by route generation.

**Tech Stack:** React 18, TypeScript, React Router 6, Axios, Vitest, React Testing Library, Go/Gin regression suite, nginx/OCI production deployment.

---

### Task 1: Add the bounded article-detail request cache

**Files:**
- Create: `frontend/src/api/articleDetailCache.ts`
- Create: `frontend/test/articleDetailCache.test.ts`

- [ ] **Step 1: Write the failing cache tests**

Create `frontend/test/articleDetailCache.test.ts` with a controllable loader and
tests for request coalescing, LRU eviction, soft-fresh prefetch, retry after
failure, invalidation, and reset:

```ts
import { describe, expect, it, vi } from 'vitest'
import {
  ArticleDetailCache,
  type ArticleDetailLoader,
} from '../src/api/articleDetailCache'
import type { ArticleDetailResponse } from '../src/api/client'

function detail(id: number): ArticleDetailResponse {
  return {
    article: {
      id,
      feed_id: 1,
      title: `Article ${id}`,
      url: `https://example.com/${id}`,
      content: `Body ${id}`,
      published_at: '2026-07-27T00:00:00Z',
      summary_brief: '',
      summary_detailed: '',
      fetched_at: '2026-07-27T00:00:00Z',
    },
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe('ArticleDetailCache', () => {
  it('coalesces a prefetch and navigation for the same article', async () => {
    const pending = deferred<ArticleDetailResponse>()
    const loader: ArticleDetailLoader = vi.fn(() => pending.promise)
    const cache = new ArticleDetailCache(loader)

    const prefetch = cache.prefetch(7)
    const navigation = cache.fetch(7)
    expect(loader).toHaveBeenCalledTimes(1)

    pending.resolve(detail(7))
    await expect(prefetch).resolves.toEqual(detail(7))
    await expect(navigation).resolves.toEqual(detail(7))
    expect(cache.peek(7)).toEqual(detail(7))
  })

  it('keeps only the most recently used entries', async () => {
    const cache = new ArticleDetailCache(async id => detail(id), {
      maxEntries: 2,
    })
    await cache.fetch(1)
    await cache.fetch(2)
    cache.peek(1)
    await cache.fetch(3)
    expect(cache.peek(1)).toEqual(detail(1))
    expect(cache.peek(2)).toBeUndefined()
    expect(cache.peek(3)).toEqual(detail(3))
  })

  it('skips a fresh speculative request but refreshes after soft TTL', async () => {
    let now = 1_000
    const loader = vi.fn(async (id: number) => detail(id))
    const cache = new ArticleDetailCache(loader, {
      softTTLms: 100,
      now: () => now,
    })
    await cache.prefetch(4)
    await cache.prefetch(4)
    expect(loader).toHaveBeenCalledTimes(1)
    now += 101
    await cache.prefetch(4)
    expect(loader).toHaveBeenCalledTimes(2)
  })

  it('clears a rejected in-flight call so navigation can retry', async () => {
    const loader = vi.fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(detail(5))
    const cache = new ArticleDetailCache(loader)
    await expect(cache.fetch(5)).rejects.toThrow('offline')
    await expect(cache.fetch(5)).resolves.toEqual(detail(5))
    expect(loader).toHaveBeenCalledTimes(2)
  })

  it('invalidates one entry and resets all private data', async () => {
    const cache = new ArticleDetailCache(async id => detail(id))
    await cache.fetch(1)
    await cache.fetch(2)
    cache.invalidate(1)
    expect(cache.peek(1)).toBeUndefined()
    expect(cache.peek(2)).toEqual(detail(2))
    cache.reset()
    expect(cache.peek(2)).toBeUndefined()
  })

  it('does not let a pre-logout request repopulate the cache', async () => {
    const pending = deferred<ArticleDetailResponse>()
    const cache = new ArticleDetailCache(() => pending.promise)
    const request = cache.fetch(8)
    cache.reset()
    pending.resolve(detail(8))
    await request
    expect(cache.peek(8)).toBeUndefined()
  })
})
```

- [ ] **Step 2: Run the cache test and verify RED**

Run:

```bash
cd frontend
npx vitest run test/articleDetailCache.test.ts
```

Expected: FAIL because `src/api/articleDetailCache.ts` does not exist.

- [ ] **Step 3: Implement the minimal cache**

Create `frontend/src/api/articleDetailCache.ts`:

```ts
import { getArticle, type ArticleDetailResponse } from './client'

export type ArticleDetailLoader =
  (id: number) => Promise<ArticleDetailResponse>

interface CacheEntry {
  data: ArticleDetailResponse
  receivedAt: number
}

interface CacheOptions {
  maxEntries?: number
  softTTLms?: number
  now?: () => number
}

export class ArticleDetailCache {
  private readonly entries = new Map<number, CacheEntry>()
  private readonly inFlight = new Map<number, Promise<ArticleDetailResponse>>()
  private generation = 0
  private readonly maxEntries: number
  private readonly softTTLms: number
  private readonly now: () => number

  constructor(
    private readonly loader: ArticleDetailLoader,
    options: CacheOptions = {},
  ) {
    this.maxEntries = options.maxEntries ?? 30
    this.softTTLms = options.softTTLms ?? 5 * 60 * 1000
    this.now = options.now ?? Date.now
  }

  peek(id: number): ArticleDetailResponse | undefined {
    const entry = this.entries.get(id)
    if (!entry) return undefined
    this.entries.delete(id)
    this.entries.set(id, entry)
    return entry.data
  }

  fetch(id: number): Promise<ArticleDetailResponse> {
    const pending = this.inFlight.get(id)
    if (pending) return pending
    const generation = this.generation
    const request = this.loader(id)
      .then(data => {
        if (generation === this.generation) this.put(data)
        return data
      })
      .finally(() => {
        if (this.inFlight.get(id) === request) this.inFlight.delete(id)
      })
    this.inFlight.set(id, request)
    return request
  }

  prefetch(id: number): Promise<ArticleDetailResponse | undefined> {
    const entry = this.entries.get(id)
    if (entry && this.now() - entry.receivedAt <= this.softTTLms) {
      this.peek(id)
      return Promise.resolve(entry.data)
    }
    return this.fetch(id).catch(() => undefined)
  }

  put(data: ArticleDetailResponse): void {
    const id = data.article.id
    this.entries.delete(id)
    this.entries.set(id, { data, receivedAt: this.now() })
    while (this.entries.size > this.maxEntries) {
      const oldest = this.entries.keys().next().value as number | undefined
      if (oldest === undefined) break
      this.entries.delete(oldest)
    }
  }

  invalidate(id: number): void {
    this.generation += 1
    this.entries.delete(id)
    this.inFlight.delete(id)
  }

  reset(): void {
    this.generation += 1
    this.entries.clear()
    this.inFlight.clear()
  }
}

const sharedArticleDetailCache = new ArticleDetailCache(getArticle)

export const peekArticleDetail = (id: number) =>
  sharedArticleDetailCache.peek(id)
export const fetchArticleDetail = (id: number) =>
  sharedArticleDetailCache.fetch(id)
export const prefetchArticleDetail = (id: number) =>
  sharedArticleDetailCache.prefetch(id)
export const putArticleDetail = (data: ArticleDetailResponse) =>
  sharedArticleDetailCache.put(data)
export const invalidateArticleDetail = (id: number) =>
  sharedArticleDetailCache.invalidate(id)
export const resetArticleDetailCache = () =>
  sharedArticleDetailCache.reset()
```

- [ ] **Step 4: Run the cache tests and frontend typecheck**

Run:

```bash
cd frontend
npx vitest run test/articleDetailCache.test.ts
npx tsc --noEmit
```

Expected: 6 cache tests PASS and TypeScript exits 0.

- [ ] **Step 5: Commit the cache**

```bash
git add frontend/src/api/articleDetailCache.ts frontend/test/articleDetailCache.test.ts
git commit -m "feat: cache article detail requests in session"
```

### Task 2: Schedule bounded idle and interaction prefetch

**Files:**
- Create: `frontend/src/hooks/useArticleDetailPrefetch.ts`
- Create: `frontend/test/useArticleDetailPrefetch.test.tsx`
- Modify: `frontend/src/components/ArticleCard.tsx`
- Create: `frontend/test/ArticleCardPrefetch.test.tsx`

- [ ] **Step 1: Write failing scheduler and card interaction tests**

Create `frontend/test/useArticleDetailPrefetch.test.tsx`:

```tsx
import { act, render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  prefetchArticleIDs,
  useArticleDetailPrefetch,
} from '../src/hooks/useArticleDetailPrefetch'

const cacheMocks = vi.hoisted(() => ({
  prefetchArticleDetail: vi.fn(async () => undefined),
}))

vi.mock('../src/api/articleDetailCache', () => cacheMocks)

function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>(resolvePromise => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function Harness({ ids }: { ids: number[] }) {
  useArticleDetailPrefetch(ids)
  return null
}

afterEach(() => vi.unstubAllGlobals())

describe('article detail prefetch scheduling', () => {
  it('runs no more than two prefetches concurrently', async () => {
    const pending = Array.from({ length: 4 }, deferred)
    let active = 0
    let maxActive = 0
    const prefetcher = vi.fn(async (id: number) => {
      active += 1
      maxActive = Math.max(maxActive, active)
      await pending[id - 1].promise
      active -= 1
    })
    const all = prefetchArticleIDs([1, 2, 3, 4], prefetcher, 2)
    await Promise.resolve()
    expect(prefetcher).toHaveBeenCalledTimes(2)
    pending[0].resolve()
    pending[1].resolve()
    await Promise.resolve()
    await Promise.resolve()
    expect(prefetcher).toHaveBeenCalledTimes(4)
    pending[2].resolve()
    pending[3].resolve()
    await all
    expect(maxActive).toBe(2)
  })

  it('schedules only the first six IDs during browser idle time', async () => {
    cacheMocks.prefetchArticleDetail.mockClear()
    let idleCallback: IdleRequestCallback | undefined
    const cancelIdleCallback = vi.fn()
    vi.stubGlobal('requestIdleCallback', vi.fn((callback: IdleRequestCallback) => {
      idleCallback = callback
      return 17
    }))
    vi.stubGlobal('cancelIdleCallback', cancelIdleCallback)
    const { unmount } = render(<Harness ids={[1, 2, 3, 4, 5, 6, 7]} />)
    expect(idleCallback).toBeDefined()
    await act(async () => {
      idleCallback?.({ didTimeout: false, timeRemaining: () => 50 })
      await Promise.resolve()
    })
    expect(cacheMocks.prefetchArticleDetail.mock.calls.map(([id]) => id))
      .toEqual([1, 2, 3, 4, 5, 6])
    unmount()
    expect(cancelIdleCallback).toHaveBeenCalledWith(17)
  })
})
```

Create `frontend/test/ArticleCardPrefetch.test.tsx`:

```tsx
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import ArticleCard from '../src/components/ArticleCard'
import type { ArticleListItem } from '../src/api/client'

vi.mock('../src/hooks/useExposureTracking', () => ({
  useExposureTracking: () => ({ current: null }),
  reportClick: vi.fn(),
}))

describe('ArticleCard detail prefetch', () => {
  it('promotes pointer, focus, and touch interactions', () => {
    const article: ArticleListItem = {
      id: 9,
      feed_id: 1,
      title: 'Article 9',
      url: 'https://example.com/9',
      published_at: '2026-07-27T00:00:00Z',
      summary_brief: '',
      fetched_at: '2026-07-27T00:00:00Z',
      manual_tags: [],
    }
    const onPrefetch = vi.fn()
    render(
      <ArticleCard
        article={article}
        isRead={false}
        isFocused={false}
        idx={0}
        onPlay={vi.fn()}
        formatDate={() => ''}
        stripMarkdown={text => text}
        onOpen={vi.fn()}
        onFocus={vi.fn()}
        onPrefetch={onPrefetch}
      />,
    )
    const card = screen.getByText('Article 9').closest('[data-article-card]')!
    fireEvent.pointerEnter(card)
    fireEvent.focus(card)
    fireEvent.touchStart(card)
    expect(onPrefetch.mock.calls).toEqual([[9], [9], [9]])
  })
})
```

- [ ] **Step 2: Run the prefetch tests and verify RED**

Run:

```bash
cd frontend
npx vitest run test/useArticleDetailPrefetch.test.tsx test/ArticleCardPrefetch.test.tsx
```

Expected: FAIL because the hook and `onPrefetch` prop do not exist.

- [ ] **Step 3: Implement the concurrency-limited idle hook**

Create `frontend/src/hooks/useArticleDetailPrefetch.ts`:

```ts
import { useCallback, useEffect } from 'react'
import { prefetchArticleDetail } from '../api/articleDetailCache'

const IDLE_LIMIT = 6
const DEFAULT_CONCURRENCY = 2

export async function prefetchArticleIDs(
  ids: number[],
  prefetcher: (id: number) => Promise<unknown> = prefetchArticleDetail,
  concurrency = DEFAULT_CONCURRENCY,
): Promise<void> {
  let next = 0
  const worker = async () => {
    while (next < ids.length) {
      const id = ids[next++]
      await prefetcher(id)
    }
  }
  await Promise.all(
    Array.from(
      { length: Math.min(Math.max(1, concurrency), ids.length) },
      worker,
    ),
  )
}

export function useArticleDetailPrefetch(ids: number[]) {
  const key = ids.slice(0, IDLE_LIMIT).join(',')

  useEffect(() => {
    const selected = key ? key.split(',').map(Number) : []
    if (selected.length === 0) return
    const start = () => { void prefetchArticleIDs(selected) }
    if ('requestIdleCallback' in window) {
      const idleID = window.requestIdleCallback(start, { timeout: 1000 })
      return () => window.cancelIdleCallback(idleID)
    }
    const timerID = window.setTimeout(start, 0)
    return () => window.clearTimeout(timerID)
  }, [key])

  return useCallback((id: number) => {
    void prefetchArticleDetail(id)
  }, [])
}
```

- [ ] **Step 4: Add interaction promotion to `ArticleCard`**

Add optional `onPrefetch?: (id: number) => void` to `Props`. Define a shared
handler and attach it to both tweet and regular card roots:

```tsx
const handlePrefetch = () => onPrefetch?.(article.id)

<div
  ...
  onPointerEnter={handlePrefetch}
  onFocus={handlePrefetch}
  onTouchStart={handlePrefetch}
>
```

Keep `onPrefetch` optional so grouped, clip, and recommendation callers retain
their current behavior until explicitly wired.

- [ ] **Step 5: Run the focused tests and full frontend check**

Run:

```bash
cd frontend
npx vitest run test/useArticleDetailPrefetch.test.tsx test/ArticleCardPrefetch.test.tsx
npm run check
```

Expected: focused tests PASS; all frontend tests PASS.

- [ ] **Step 6: Commit bounded prefetch scheduling**

```bash
git add frontend/src/hooks/useArticleDetailPrefetch.ts \
  frontend/src/components/ArticleCard.tsx \
  frontend/test/useArticleDetailPrefetch.test.tsx \
  frontend/test/ArticleCardPrefetch.test.tsx
git commit -m "feat: prefetch visible article details"
```

### Task 3: Seed navigation from cache or list preview

**Files:**
- Create: `frontend/src/components/ArticleDetailPreview.tsx`
- Create: `frontend/test/ArticleDetailPreview.test.tsx`
- Modify: `frontend/src/pages/ArticleListPage.tsx`
- Modify: `frontend/src/pages/ArticlePage.tsx`
- Modify: `frontend/test/ArticleListPageInfiniteScroll.test.tsx`

- [ ] **Step 1: Write failing preview and list wiring tests**

Create `frontend/test/ArticleDetailPreview.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import ArticleDetailPreview from '../src/components/ArticleDetailPreview'
import type { ArticleListItem } from '../src/api/client'

describe('ArticleDetailPreview', () => {
  it('shows list metadata without pretending the body is loaded', () => {
    const preview: ArticleListItem = {
      id: 7,
      feed_id: 1,
      feed_title: 'Example Feed',
      title: 'Prefetched title',
      url: 'https://example.com/7',
      published_at: '2026-07-27T00:00:00Z',
      summary_brief: 'Brief from the list',
      fetched_at: '2026-07-27T00:00:00Z',
      manual_tags: [],
    }
    render(<ArticleDetailPreview article={preview} />)
    expect(screen.getByText('Prefetched title')).toBeTruthy()
    expect(screen.getByText('Example Feed')).toBeTruthy()
    expect(screen.getByText('Brief from the list')).toBeTruthy()
    expect(screen.getByText('正在加载正文…')).toBeTruthy()
  })
})
```

Extend `frontend/test/ArticleListPageInfiniteScroll.test.tsx`:

```tsx
import {
  MemoryRouter, Route, Routes, useLocation,
} from 'react-router-dom'

const detailCacheMocks = vi.hoisted(() => ({
  prefetchArticleDetail: vi.fn(async () => undefined),
}))

vi.mock('../src/api/articleDetailCache', () => detailCacheMocks)

vi.mock('../src/components/ArticleCard', () => ({
  default: ({
    article,
    prefetchRef,
    onOpen,
    onPrefetch,
  }: {
    article: ArticleListItem
    prefetchRef?: React.RefObject<HTMLDivElement>
    onOpen: (id: number, preview: ArticleListItem) => void
    onPrefetch?: (id: number) => void
  }) => (
    <button
      ref={prefetchRef as React.RefObject<HTMLButtonElement>}
      data-testid={`article-${article.id}`}
      onMouseEnter={() => onPrefetch?.(article.id)}
      onClick={() => onOpen(article.id, article)}
    >
      {article.title}
    </button>
  ),
}))

function ArticleLocationProbe() {
  const location = useLocation()
  const state = location.state as {
    articlePreview?: ArticleListItem
  } | null
  return (
    <output data-testid="route-preview">
      {state?.articlePreview?.id ?? 'missing'}
    </output>
  )
}

it('prefetches card interaction and hands its preview to the route', async () => {
  apiMocks.getArticles.mockImplementation(({ offset }: { offset: number }) =>
    Promise.resolve(offset === 0 ? makeArticles(1) : []),
  )
  render(
    <MemoryRouter initialEntries={['/articles']}>
      <Routes>
        <Route path="/articles" element={<ArticleListPage />} />
        <Route path="/articles/:id" element={<ArticleLocationProbe />} />
      </Routes>
    </MemoryRouter>,
  )
  const first = await screen.findByTestId('article-1')
  fireEvent.mouseEnter(first)
  expect(detailCacheMocks.prefetchArticleDetail).toHaveBeenCalledWith(1)
  fireEvent.click(first)
  expect((await screen.findByTestId('route-preview')).textContent).toBe('1')
})
```

The scheduler unit test owns the six-item/concurrency contract; this integration
test owns the card interaction and route-preview wiring.

- [ ] **Step 2: Run preview/list tests and verify RED**

Run:

```bash
cd frontend
npx vitest run test/ArticleDetailPreview.test.tsx \
  test/ArticleListPageInfiniteScroll.test.tsx
```

Expected: FAIL because the preview component and cache/list wiring are absent.

- [ ] **Step 3: Implement the preview component**

Create `frontend/src/components/ArticleDetailPreview.tsx`:

```tsx
import type { ArticleListItem } from '../api/client'

export default function ArticleDetailPreview({
  article,
}: {
  article: ArticleListItem
}) {
  return (
    <div className="card" aria-live="polite">
      <h2 style={{ marginTop: 0 }}>{article.title}</h2>
      <div className="text-muted text-sm">
        {[article.feed_title, article.published_at
          ? new Date(article.published_at).toLocaleString('zh-CN')
          : ''].filter(Boolean).join(' · ')}
      </div>
      {article.summary_brief && (
        <p className="text-muted">{article.summary_brief}</p>
      )}
      <div className="text-muted text-sm">正在加载正文…</div>
    </div>
  )
}
```

- [ ] **Step 4: Wire bounded prefetch and preview state in the list**

In `ArticleListPage`:

```ts
import { useArticleDetailPrefetch } from '../hooks/useArticleDetailPrefetch'
```

After list state is established:

```ts
const promoteArticlePrefetch = useArticleDetailPrefetch(
  articles.slice(0, 6).map(article => article.id),
)
```

Change `openArticle` to accept a preview:

```ts
const openArticle = (
  id: number,
  articlePreview?: ArticleListItem | Article,
) => {
  // existing nav snapshot and scroll persistence
  navigate(`/articles/${id}`, {
    state: { from: '/articles', articlePreview },
  })
}
```

In `ArticleCard.tsx`, widen the callback without breaking callers that ignore
the second argument:

```ts
onOpen: (id: number, preview: ArticleCardItem) => void
```

and change the click:

```ts
onOpen(article.id, article)
```

Pass `onPrefetch={promoteArticlePrefetch}` to regular `ArticleCard` rows.
Search results pass their own `ArticleListItem` in the same route state.

- [ ] **Step 5: Seed and revalidate in `ArticlePage`**

Import:

```ts
import type { ArticleListItem } from '../api/client'
import ArticleDetailPreview from '../components/ArticleDetailPreview'
import {
  fetchArticleDetail,
  peekArticleDetail,
} from '../api/articleDetailCache'
```

Define route state and initialize from cache:

```ts
type ArticleLocationState = {
  from?: string
  articlePreview?: ArticleListItem
}

const routeID = Number(id)
const routeState = location.state as ArticleLocationState | null
const preview = routeState?.articlePreview?.id === routeID
  ? routeState.articlePreview
  : undefined
const initialDetailRef = useRef(
  Number.isFinite(routeID) ? peekArticleDetail(routeID) : undefined,
)
const initialDetail = initialDetailRef.current
```

Initialize `article`, progress, flags, children, and signals from
`initialDetail`. Extract the repeated response-to-state assignments into
`applyDetailResponse(data)`.

Rewrite `loadArticle` so it:

1. increments `loadGenerationRef`;
2. applies a route cache entry immediately when available;
3. sets blocking loading only when no cache entry exists;
4. calls `fetchArticleDetail(routeID)` unconditionally;
5. applies the response only when generation and route ID still match;
6. retains cached content and sets `refreshError` on background failure.

Use this blocking branch:

```tsx
if (loading && !article) {
  return preview
    ? <ArticleDetailPreview article={preview} />
    : <div className="card"><ArticleDetailSkeleton /></div>
}
```

Remove the old `if (loading) return <div ...>Loading...</div>` branch so cached
content remains rendered during revalidation. Add a non-blocking refresh-error
banner with a retry button above the article content.

- [ ] **Step 6: Run focused tests and the production build**

Run:

```bash
cd frontend
npx vitest run test/articleDetailCache.test.ts \
  test/useArticleDetailPrefetch.test.tsx \
  test/ArticleCardPrefetch.test.tsx \
  test/ArticleDetailPreview.test.tsx \
  test/ArticleListPageInfiniteScroll.test.tsx
npm run build
```

Expected: all focused tests PASS and Vite production build exits 0.

- [ ] **Step 7: Commit instant cache/preview rendering**

```bash
git add frontend/src/components/ArticleDetailPreview.tsx \
  frontend/src/pages/ArticleListPage.tsx \
  frontend/src/pages/ArticlePage.tsx \
  frontend/test/ArticleDetailPreview.test.tsx \
  frontend/test/ArticleListPageInfiniteScroll.test.tsx
git commit -m "feat: render prefetched articles immediately"
```

### Task 4: Keep cache private and coherent across mutations

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`
- Modify: `frontend/src/App.tsx`
- Create: `frontend/src/api/privateSession.ts`
- Create: `frontend/test/articleDetailCacheLogout.test.tsx`

- [ ] **Step 1: Write the failing logout reset test**

Create `frontend/test/articleDetailCacheLogout.test.tsx`:

```tsx
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  logout: vi.fn(),
  resetArticleDetailCache: vi.fn(),
}))

vi.mock('../src/api/client', () => ({ logout: mocks.logout }))
vi.mock('../src/api/articleDetailCache', () => ({
  resetArticleDetailCache: mocks.resetArticleDetailCache,
}))

import { clearPrivateSessionState } from '../src/api/privateSession'

describe('private session cleanup', () => {
  beforeEach(() => vi.clearAllMocks())

  it('clears cached article bodies before auth logout', () => {
    const order: string[] = []
    mocks.resetArticleDetailCache.mockImplementation(() => order.push('cache'))
    mocks.logout.mockImplementation(() => order.push('auth'))
    clearPrivateSessionState()
    expect(order).toEqual(['cache', 'auth'])
  })
})
```

- [ ] **Step 2: Run the logout test and verify RED**

Run:

```bash
cd frontend
npx vitest run test/articleDetailCacheLogout.test.tsx
```

Expected: FAIL because logout does not reset the article-detail cache.

- [ ] **Step 3: Reset cache on logout**

Create `frontend/src/api/privateSession.ts`:

```ts
import { resetArticleDetailCache } from './articleDetailCache'
import { logout } from './client'

export function clearPrivateSessionState(): void {
  resetArticleDetailCache()
  logout()
}
```

In `App.tsx`, import `clearPrivateSessionState` and use it from `handleLogout`
before setting the user to null and navigating to `/login`:

```ts
const handleLogout = () => {
  clearPrivateSessionState()
  setUser(null)
  window.location.href = '/login'
}
```

Leave the current `Layout` callback contract unchanged.

- [ ] **Step 4: Invalidate or refresh after article mutations**

In `ArticlePage.tsx`, replace all remaining `getArticle(...)` calls with
`fetchArticleDetail(...)`.

Call `invalidateArticleDetail(article.id)` after successful mutations whose
response is not a complete detail representation:

- AI summary completion;
- content fetch;
- like/dislike/save/unsave;
- hide/unhide;
- progress update/reset;
- link-set expand/batch refresh before fetching the new complete response.

When a complete `ArticleDetailResponse` is fetched, the cache module updates it
automatically. Do not construct partial cached responses.

- [ ] **Step 5: Run all frontend and backend verification**

Run:

```bash
cd frontend
npm run check
npm run build
cd ../backend
go test ./... -count=1
```

Expected: 0 test failures and a successful production build. Existing Vite
bundle-size warnings are allowed; new errors or warnings from the changed code
are not.

- [ ] **Step 6: Commit mutation and logout coherence**

```bash
git add frontend/src/App.tsx frontend/src/api/privateSession.ts \
  frontend/src/pages/ArticlePage.tsx \
  frontend/test/articleDetailCacheLogout.test.tsx
git commit -m "fix: invalidate article cache after private changes"
```

### Task 5: Integrate, deploy, and measure production speed

**Files:**
- Modify only if verification finds a defect in the files above.

- [ ] **Step 1: Review the branch diff and rerun clean verification**

Run:

```bash
git status --short
git diff --check master...HEAD
git diff --stat master...HEAD
cd frontend && npm run check && npm run build
cd ../backend && go test ./... -count=1
```

Expected: clean worktree, no whitespace errors, all tests/builds pass.

- [ ] **Step 2: Integrate into `master` and push**

From the main worktree:

```bash
git merge --ff-only feat/article-detail-prefetch
git push origin master
```

Expected: `origin/master` advances to the verified feature head.

- [ ] **Step 3: Deploy through the existing OCI entrypoint**

Run:

```bash
ssh oci-rss-pal 'cd /opt/rss-pal && bash scripts/auto_deploy.sh'
```

Expected: remote checkout advances to the pushed commit, frontend/API/worker
containers are healthy, and the deploy health check returns HTTP 200.

- [ ] **Step 4: Verify runtime and request behavior**

Run:

```bash
ssh oci-rss-pal 'cd /opt/rss-pal && git rev-parse --short HEAD && \
  docker compose ps && curl -fsS http://localhost:8080/api/health'
```

Use an authenticated browser session to confirm:

- idle list prefetch produces at most two concurrent detail requests;
- opening a prefetched article renders the body before its revalidation ends;
- opening an immediate cold article shows its list preview without a blank
  card;
- returning and reopening renders from session memory;
- logout and login cannot show the previous user's cached article.

- [ ] **Step 5: Collect the production timing matrix**

For three representative articles, record five samples each for cold,
prefetched, and repeat navigation. Use `performance.now()` around click and the
first full-content render, browser Network timing, nginx access logs, and Gin
duration logs.

Expected:

- prefetched median click-to-full-content below 100 ms;
- repeat median click-to-full-content below 100 ms;
- cold click-to-preview within one animation frame;
- API handler remains in the previously observed 5–15 ms range;
- no extra duplicate `/api/articles/:id` calls.

- [ ] **Step 6: Record results before phase-two infrastructure work**

Summarize median/range and note residual cold-network latency. Use that evidence
to choose between host nginx HTTP/2/HTTP/3, a nearer reverse proxy, or a CDN in
the next phase.
