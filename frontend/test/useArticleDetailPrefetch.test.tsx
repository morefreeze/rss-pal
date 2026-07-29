import { act, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  prefetchArticleIDs,
  useArticleDetailPrefetch,
  type ArticleDetailPrefetchHandle,
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

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

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
    await waitFor(() => expect(prefetcher).toHaveBeenCalledTimes(4))
    pending[2].resolve()
    pending[3].resolve()
    await all
    expect(maxActive).toBe(2)
  })

  it('schedules only the first six IDs during browser idle time', async () => {
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
    await waitFor(() => {
      expect(cacheMocks.prefetchArticleDetail.mock.calls.map(([id]) => id))
        .toEqual([1, 2, 3, 4, 5, 6])
    })
    unmount()
    expect(cancelIdleCallback).toHaveBeenCalledWith(17)
  })

  it('still exposes a hover/focus/touch promote callback that prefetches directly', () => {
    let handle: ArticleDetailPrefetchHandle | undefined
    function PromoteHarness() {
      handle = useArticleDetailPrefetch([])
      return null
    }
    render(<PromoteHarness />)
    handle?.promote(42)
    expect(cacheMocks.prefetchArticleDetail).toHaveBeenCalledWith(42)
  })
})

describe('viewport-driven (scroll) prefetch', () => {
  let observerInstances: TestIntersectionObserver[] = []

  class TestIntersectionObserver implements IntersectionObserver {
    readonly root: Element | Document | null = null
    readonly rootMargin: string
    readonly thresholds: ReadonlyArray<number> = [0]
    observe = vi.fn()
    unobserve = vi.fn()
    disconnect = vi.fn()
    takeRecords = vi.fn((): IntersectionObserverEntry[] => [])

    constructor(readonly callback: IntersectionObserverCallback, options: IntersectionObserverInit = {}) {
      this.rootMargin = options.rootMargin ?? '0px'
      observerInstances.push(this)
    }
  }

  beforeEach(() => {
    observerInstances = []
    vi.stubGlobal('IntersectionObserver', TestIntersectionObserver)
    vi.useFakeTimers()
    cacheMocks.prefetchArticleDetail.mockReset()
    cacheMocks.prefetchArticleDetail.mockImplementation(async () => undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  function mountHarness(enabled = true) {
    let handle!: ArticleDetailPrefetchHandle
    function Harness() {
      handle = useArticleDetailPrefetch([], enabled)
      return null
    }
    render(<Harness />)
    return handle
  }

  function fireIntersection(target: Element, isIntersecting: boolean) {
    const observer = observerInstances[observerInstances.length - 1]
    observer.callback(
      [{ target, isIntersecting } as IntersectionObserverEntry],
      observer,
    )
  }

  const advance = (ms: number) => act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })

  // Flushes chained promise continuations (await + .finally hops) without
  // relying on real timers, since this describe block runs under fake ones.
  const flushMicrotasks = (hops = 10) => act(async () => {
    for (let i = 0; i < hops; i++) {
      await Promise.resolve()
    }
  })

  it('prefetches a card that dwells past the 250ms delay exactly once', async () => {
    const { registerCard } = mountHarness()
    const el = document.createElement('div')
    registerCard(1, el)

    fireIntersection(el, true)
    expect(cacheMocks.prefetchArticleDetail).not.toHaveBeenCalled()

    await advance(250)

    expect(cacheMocks.prefetchArticleDetail).toHaveBeenCalledTimes(1)
    expect(cacheMocks.prefetchArticleDetail).toHaveBeenCalledWith(1)
  })

  it('does not prefetch a card that leaves the viewport before the dwell delay elapses (fast scroll)', async () => {
    const { registerCard } = mountHarness()
    const el = document.createElement('div')
    registerCard(1, el)

    fireIntersection(el, true)
    await advance(150)
    fireIntersection(el, false)
    await advance(250)

    expect(cacheMocks.prefetchArticleDetail).not.toHaveBeenCalled()
  })

  it('never runs more than two viewport prefetches concurrently', async () => {
    const pending = Array.from({ length: 4 }, deferred)
    let active = 0
    let maxActive = 0
    cacheMocks.prefetchArticleDetail.mockImplementation(async (id: number) => {
      active += 1
      maxActive = Math.max(maxActive, active)
      await pending[id - 1].promise
      active -= 1
    })

    const { registerCard } = mountHarness()
    const elements = [1, 2, 3, 4].map(id => {
      const el = document.createElement('div')
      registerCard(id, el)
      return el
    })
    elements.forEach(el => fireIntersection(el, true))

    await advance(250)
    expect(cacheMocks.prefetchArticleDetail).toHaveBeenCalledTimes(2)

    pending[0].resolve()
    pending[1].resolve()
    await flushMicrotasks()
    expect(cacheMocks.prefetchArticleDetail).toHaveBeenCalledTimes(4)

    pending[2].resolve()
    pending[3].resolve()
    await flushMicrotasks()
    expect(maxActive).toBe(2)
  })

  it('does not refire for repeat registrations of the same id', async () => {
    const { registerCard } = mountHarness()
    const elA = document.createElement('div')
    registerCard(1, elA)
    fireIntersection(elA, true)
    await advance(250)
    expect(cacheMocks.prefetchArticleDetail).toHaveBeenCalledTimes(1)

    // Same element registered again (e.g. a re-render calling the ref
    // callback again with the identical node) must not re-observe or refire.
    registerCard(1, elA)
    fireIntersection(elA, true)
    await advance(250)
    expect(cacheMocks.prefetchArticleDetail).toHaveBeenCalledTimes(1)

    // Unregister then register a fresh element under the same id (e.g. the
    // card unmounts and remounts within the same page mount) — the id is
    // already in the dedup set for this mount, so it still must not refire.
    registerCard(1, null)
    const elB = document.createElement('div')
    registerCard(1, elB)
    fireIntersection(elB, true)
    await advance(250)
    expect(cacheMocks.prefetchArticleDetail).toHaveBeenCalledTimes(1)
  })

  it('skips viewport prefetch entirely when the connection reports save-data', async () => {
    Object.defineProperty(navigator, 'connection', {
      value: { saveData: true },
      configurable: true,
    })
    try {
      const { registerCard } = mountHarness()
      const el = document.createElement('div')
      registerCard(1, el)
      fireIntersection(el, true)
      await advance(250)
      expect(cacheMocks.prefetchArticleDetail).not.toHaveBeenCalled()
    } finally {
      delete (navigator as { connection?: unknown }).connection
    }
  })

  it('skips viewport prefetch entirely on 2g/slow-2g effective connection types', async () => {
    Object.defineProperty(navigator, 'connection', {
      value: { effectiveType: '2g' },
      configurable: true,
    })
    try {
      const { registerCard } = mountHarness()
      const el = document.createElement('div')
      registerCard(1, el)
      fireIntersection(el, true)
      await advance(250)
      expect(cacheMocks.prefetchArticleDetail).not.toHaveBeenCalled()
    } finally {
      delete (navigator as { connection?: unknown }).connection
    }
  })

  it('skips viewport prefetch when disabled (e.g. clip mode) while leaving promote usable', async () => {
    const { registerCard, promote } = mountHarness(false)
    const el = document.createElement('div')
    registerCard(1, el)
    fireIntersection(el, true)
    await advance(250)
    expect(cacheMocks.prefetchArticleDetail).not.toHaveBeenCalled()

    promote(2)
    expect(cacheMocks.prefetchArticleDetail).toHaveBeenCalledWith(2)
  })

  it('cancels the pending dwell timer when a card unregisters before it elapses', async () => {
    const { registerCard } = mountHarness()
    const el = document.createElement('div')
    registerCard(1, el)
    fireIntersection(el, true)
    await advance(150)
    registerCard(1, null)
    await advance(250)

    expect(cacheMocks.prefetchArticleDetail).not.toHaveBeenCalled()
  })
})
