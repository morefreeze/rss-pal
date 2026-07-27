import { act, render, waitFor } from '@testing-library/react'
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
})
