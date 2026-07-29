import { getArticle, type ArticleDetailResponse } from './client'

export type ArticleDetailLoader = (id: number) => Promise<ArticleDetailResponse>

interface CacheEntry {
  data: ArticleDetailResponse
  receivedAt: number
}

interface CacheOptions {
  maxEntries?: number
  softTTLms?: number
  now?: () => number
  useStorage?: boolean
}

interface StorageIndexEntry {
  id: number
  cachedAt: number
}

interface PersistedCacheEntry {
  data: ArticleDetailResponse
  receivedAt: number
}

const STORAGE_KEY_PREFIX = 'rss-pal:article-detail:'
const STORAGE_INDEX_KEY = 'rss-pal:article-detail:index'

export class ArticleDetailCache {
  private readonly entries = new Map<number, CacheEntry>()
  private readonly inFlight = new Map<number, Promise<ArticleDetailResponse>>()
  private generation = 0
  private readonly maxEntries: number
  private readonly softTTLms: number
  private readonly now: () => number
  private readonly useStorage: boolean

  constructor(
    private readonly loader: ArticleDetailLoader,
    options: CacheOptions = {},
  ) {
    this.maxEntries = options.maxEntries ?? 30
    this.softTTLms = options.softTTLms ?? 5 * 60 * 1000
    this.now = options.now ?? Date.now
    this.useStorage = options.useStorage ?? true
  }

  peek(id: number): ArticleDetailResponse | undefined {
    let entry = this.entries.get(id)

    // If not in memory, try to hydrate from storage
    if (!entry && this.useStorage) {
      entry = this.hydrateFromStorage(id)
      if (!entry) return undefined
    } else if (!entry) {
      return undefined
    }

    // Move to end (MRU)
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
    let entry = this.entries.get(id)

    // If not in memory, try to hydrate from storage
    if (!entry && this.useStorage) {
      entry = this.hydrateFromStorage(id)
    }

    if (entry && this.now() - entry.receivedAt <= this.softTTLms) {
      this.peek(id)
      return Promise.resolve(entry.data)
    }
    return this.fetch(id).catch(() => undefined)
  }

  put(data: ArticleDetailResponse): void {
    const id = data.article.id
    this.entries.delete(id)
    const entry = { data, receivedAt: this.now() }
    this.entries.set(id, entry)

    // Persist to storage
    if (this.useStorage) {
      this.saveToStorage(id, entry)
    }

    // Enforce in-memory size limit and sync evictions to storage
    while (this.entries.size > this.maxEntries) {
      const oldest = this.entries.keys().next().value as number | undefined
      if (oldest === undefined) break
      this.entries.delete(oldest)
      if (this.useStorage) {
        this.removeFromStorage(oldest)
      }
    }
  }

  invalidate(id: number): void {
    this.generation += 1
    this.entries.delete(id)
    this.inFlight.delete(id)
    if (this.useStorage) {
      this.removeFromStorage(id)
    }
  }

  reset(): void {
    this.generation += 1
    this.entries.clear()
    this.inFlight.clear()
    if (this.useStorage) {
      this.clearAllStorage()
    }
  }

  // ===== Storage helpers =====

  private hydrateFromStorage(id: number): CacheEntry | undefined {
    const entry = this.loadFromStorage(id)
    if (!entry) return undefined

    // Add to in-memory map
    this.entries.set(id, entry)

    // Enforce size limit
    while (this.entries.size > this.maxEntries) {
      const oldest = this.entries.keys().next().value as number | undefined
      if (oldest === undefined) break
      this.entries.delete(oldest)
      this.removeFromStorage(oldest)
    }

    return entry
  }

  private loadFromStorage(id: number): CacheEntry | undefined {
    try {
      const key = STORAGE_KEY_PREFIX + id
      const stored = localStorage.getItem(key)
      if (!stored) return undefined

      const parsed = JSON.parse(stored) as PersistedCacheEntry
      return {
        data: parsed.data,
        receivedAt: parsed.receivedAt,
      }
    } catch {
      // Silently ignore storage errors
      return undefined
    }
  }

  private saveToStorage(id: number, entry: CacheEntry): void {
    try {
      const key = STORAGE_KEY_PREFIX + id
      const toStore: PersistedCacheEntry = {
        data: entry.data,
        receivedAt: entry.receivedAt,
      }
      localStorage.setItem(key, JSON.stringify(toStore))
      this.updateStorageIndex(id)
    } catch (err) {
      // Handle quota exceeded or other storage errors
      if (err instanceof Error && err.name === 'QuotaExceededError') {
        try {
          // Try to evict the oldest entry and retry once
          const removed = this.evictOldestFromStorage()
          if (removed) {
            const key = STORAGE_KEY_PREFIX + id
            const toStore: PersistedCacheEntry = {
              data: entry.data,
              receivedAt: entry.receivedAt,
            }
            localStorage.setItem(key, JSON.stringify(toStore))
            this.updateStorageIndex(id)
          }
        } catch {
          // Give up silently
        }
      }
      // Silently ignore other storage errors
    }
  }

  private removeFromStorage(id: number): void {
    try {
      const key = STORAGE_KEY_PREFIX + id
      localStorage.removeItem(key)
      const index = this.loadStorageIndex().filter(entry => entry.id !== id)
      localStorage.setItem(STORAGE_INDEX_KEY, JSON.stringify(index))
    } catch {
      // Silently ignore storage errors
    }
  }

  private updateStorageIndex(id: number): void {
    try {
      const index = this.loadStorageIndex()
      const existing = index.findIndex(entry => entry.id === id)
      if (existing !== -1) {
        index.splice(existing, 1)
      }
      index.push({ id, cachedAt: this.now() })

      // Enforce storage limit
      while (index.length > this.maxEntries) {
        const oldest = index.shift()
        if (oldest) {
          localStorage.removeItem(STORAGE_KEY_PREFIX + oldest.id)
        }
      }

      localStorage.setItem(STORAGE_INDEX_KEY, JSON.stringify(index))
    } catch {
      // Silently ignore storage errors
    }
  }

  private loadStorageIndex(): StorageIndexEntry[] {
    try {
      const stored = localStorage.getItem(STORAGE_INDEX_KEY)
      if (!stored) return []
      return JSON.parse(stored) as StorageIndexEntry[]
    } catch {
      return []
    }
  }

  private evictOldestFromStorage(): boolean {
    try {
      const index = this.loadStorageIndex()
      if (index.length === 0) return false

      const oldest = index.shift()
      if (!oldest) return false

      localStorage.removeItem(STORAGE_KEY_PREFIX + oldest.id)
      localStorage.setItem(STORAGE_INDEX_KEY, JSON.stringify(index))
      return true
    } catch {
      return false
    }
  }

  private clearAllStorage(): void {
    try {
      const index = this.loadStorageIndex()
      for (const entry of index) {
        localStorage.removeItem(STORAGE_KEY_PREFIX + entry.id)
      }
      localStorage.removeItem(STORAGE_INDEX_KEY)
    } catch {
      // Silently ignore storage errors
    }
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
