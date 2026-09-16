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

function accountScope(): string {
  try {
    const id = JSON.parse(localStorage.getItem('user') || 'null')?.id
    return Number.isSafeInteger(id) && id > 0 ? `user-${id}` : 'anonymous'
  } catch {
    return 'anonymous'
  }
}

export function isArticleAccessDenied(error: unknown): boolean {
  const status = (error as { response?: { status?: number } })?.response?.status
  return status === 401 || status === 403 || status === 404
}

export class ArticleDetailCache {
  private readonly entries = new Map<number, CacheEntry>()
  private readonly inFlight = new Map<number, Promise<ArticleDetailResponse>>()
  private generation = 0
  private scope = accountScope()
  private get storagePrefix() { return `rss-pal:article-detail:v2:${this.scope}:` }
  private get storageIndexKey() { return this.storagePrefix + 'index' }

  private syncAccount(): void {
    const scope = accountScope()
    if (scope === this.scope) return
    this.scope = scope
    this.generation += 1
    this.entries.clear()
    this.inFlight.clear()
  }
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
    // Discard the old cache whose entries have no trustworthy owner metadata.
    if (this.useStorage) {
      try {
        for (const key of Object.keys(localStorage)) {
          if (/^rss-pal:article-detail:(index|[0-9]+)$/.test(key)) localStorage.removeItem(key)
        }
      } catch { /* Storage can be disabled. */ }
    }
  }

  peek(id: number): ArticleDetailResponse | undefined {
    this.syncAccount()
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
    this.syncAccount()
    const pending = this.inFlight.get(id)
    if (pending) return pending

    const generation = this.generation
    const scope = this.scope
    const request = this.loader(id)
      .then(data => {
        this.syncAccount()
        if (scope !== this.scope) {
          throw Object.assign(new Error('Account changed'), { response: { status: 403 } })
        }
        if (generation === this.generation) this.put(data)
        return data
      })
      .catch(error => {
        this.syncAccount()
        if (scope === this.scope && isArticleAccessDenied(error)) this.invalidate(id)
        throw error
      })
      .finally(() => {
        if (this.inFlight.get(id) === request) this.inFlight.delete(id)
      })
    this.inFlight.set(id, request)
    return request
  }

  prefetch(id: number): Promise<ArticleDetailResponse | undefined> {
    this.syncAccount()
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
    this.syncAccount()
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
    this.syncAccount()
    this.generation += 1
    this.entries.delete(id)
    this.inFlight.delete(id)
    if (this.useStorage) {
      this.removeFromStorage(id)
    }
  }

  reset(): void {
    this.syncAccount()
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
      const key = this.storagePrefix + id
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
      const key = this.storagePrefix + id
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
            const key = this.storagePrefix + id
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
      const key = this.storagePrefix + id
      localStorage.removeItem(key)
      const index = this.loadStorageIndex().filter(entry => entry.id !== id)
      localStorage.setItem(this.storageIndexKey, JSON.stringify(index))
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
          localStorage.removeItem(this.storagePrefix + oldest.id)
        }
      }

      localStorage.setItem(this.storageIndexKey, JSON.stringify(index))
    } catch {
      // Silently ignore storage errors
    }
  }

  private loadStorageIndex(): StorageIndexEntry[] {
    try {
      const stored = localStorage.getItem(this.storageIndexKey)
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

      localStorage.removeItem(this.storagePrefix + oldest.id)
      localStorage.setItem(this.storageIndexKey, JSON.stringify(index))
      return true
    } catch {
      return false
    }
  }

  private clearAllStorage(): void {
    try {
      const index = this.loadStorageIndex()
      for (const entry of index) {
        localStorage.removeItem(this.storagePrefix + entry.id)
      }
      localStorage.removeItem(this.storageIndexKey)
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
