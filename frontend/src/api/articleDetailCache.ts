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
