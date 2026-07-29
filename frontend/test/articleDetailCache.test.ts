import { beforeEach, describe, expect, it, vi } from 'vitest'
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

describe('ArticleDetailCache with storage', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('persists entries to localStorage and retrieves them on peek', async () => {
    const cache = new ArticleDetailCache(async id => detail(id), {
      useStorage: true,
    })

    await cache.fetch(1)
    expect(cache.peek(1)).toEqual(detail(1))

    // Create new cache instance to simulate page reload
    const cache2 = new ArticleDetailCache(async id => detail(id), {
      useStorage: true,
    })

    // Should retrieve from localStorage without network call
    const peekResult = cache2.peek(1)
    expect(peekResult).toEqual(detail(1))
  })

  it('respects TTL when prefetching from hydrated storage', async () => {
    let now = 1_000
    const loader = vi.fn(async (id: number) => detail(id))
    const cache = new ArticleDetailCache(loader, {
      softTTLms: 100,
      now: () => now,
      useStorage: true,
    })

    await cache.fetch(1)

    // Create new cache instance
    const cache2 = new ArticleDetailCache(loader, {
      softTTLms: 100,
      now: () => now,
      useStorage: true,
    })

    // Within TTL: should use hydrated storage without refetch
    let result = await cache2.prefetch(1)
    expect(result).toEqual(detail(1))
    expect(loader).toHaveBeenCalledTimes(1) // Only first fetch

    // Past TTL: should trigger a network refetch
    now += 101
    result = await cache2.prefetch(1)
    expect(result).toEqual(detail(1))
    expect(loader).toHaveBeenCalledTimes(2) // Second fetch triggered
  })

  it('removes entries from storage on invalidate', async () => {
    const cache = new ArticleDetailCache(async id => detail(id), {
      useStorage: true,
    })

    await cache.fetch(1)
    cache.invalidate(1)

    // Create new cache and verify entry is gone
    const cache2 = new ArticleDetailCache(async id => detail(id), {
      useStorage: true,
    })
    expect(cache2.peek(1)).toBeUndefined()
  })

  it('clears storage on reset', async () => {
    const cache = new ArticleDetailCache(async id => detail(id), {
      useStorage: true,
    })

    await cache.fetch(1)
    await cache.fetch(2)
    cache.reset()

    // Create new cache and verify all entries are gone
    const cache2 = new ArticleDetailCache(async id => detail(id), {
      useStorage: true,
    })
    expect(cache2.peek(1)).toBeUndefined()
    expect(cache2.peek(2)).toBeUndefined()
  })

  it('maintains storage size limit matching in-memory limit', async () => {
    const cache = new ArticleDetailCache(async id => detail(id), {
      maxEntries: 2,
      useStorage: true,
    })

    await cache.fetch(1)
    await cache.fetch(2)
    await cache.fetch(3) // Should evict 1 from both memory and storage

    expect(cache.peek(1)).toBeUndefined()
    expect(cache.peek(2)).toEqual(detail(2))
    expect(cache.peek(3)).toEqual(detail(3))

    // Verify storage also lost entry 1
    const cache2 = new ArticleDetailCache(async id => detail(id), {
      maxEntries: 2,
      useStorage: true,
    })
    expect(cache2.peek(1)).toBeUndefined()
    expect(cache2.peek(2)).toEqual(detail(2))
    expect(cache2.peek(3)).toEqual(detail(3))
  })

  it('handles storage gracefully when disabled', async () => {
    const cache = new ArticleDetailCache(async id => detail(id), {
      useStorage: false,
    })

    await cache.fetch(1)

    const cache2 = new ArticleDetailCache(async id => detail(id), {
      useStorage: false,
    })

    // Should not retrieve from storage when useStorage is false
    expect(cache2.peek(1)).toBeUndefined()
  })

  it('handles corrupted storage data silently', async () => {
    // Set corrupted data
    localStorage.setItem('rss-pal:article-detail:1', '{invalid json}')

    const cache = new ArticleDetailCache(async id => detail(id), {
      useStorage: true,
    })

    // Should not throw and should return undefined
    expect(cache.peek(1)).toBeUndefined()
  })

  it('recovers from QuotaExceededError by evicting oldest entry', async () => {
    const cache = new ArticleDetailCache(async id => detail(id), {
      useStorage: true,
    })

    // Mock localStorage.setItem to throw QuotaExceededError on first call
    const originalSetItem = localStorage.setItem
    let callCount = 0
    localStorage.setItem = vi.fn((key, value) => {
      callCount++
      if (callCount === 1 && key.startsWith('rss-pal:article-detail:')) {
        const err = new Error('QuotaExceededError')
        err.name = 'QuotaExceededError'
        throw err
      }
      originalSetItem.call(localStorage, key, value)
    })

    // Should handle the quota error gracefully
    await cache.fetch(1)
    expect(cache.peek(1)).toEqual(detail(1))

    localStorage.setItem = originalSetItem
  })
})
