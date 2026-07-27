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
      manual_tags: [],
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
