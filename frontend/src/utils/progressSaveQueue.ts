export type PendingProgress = { articleId: number; scrollPosition: number; isCompleted: boolean }
type Snapshot = { revision: string; entry: PendingProgress; generation: string }
type Persistence = { ownerId: number | null; storage?: Storage }
const localLocks = new Map<string, Promise<unknown>>()

// Numeric account identity survives token refresh and profile metadata changes.
export function progressAccountId(): number | null {
  try {
    if (!localStorage.getItem('token')) return null
    const id = JSON.parse(localStorage.getItem('user') || 'null')?.id
    return Number.isSafeInteger(id) && id > 0 ? id : null
  } catch { return null }
}

export class ProgressSaveQueue<T> {
  private pending = new Map<number, PendingProgress>()
  private operations = new Map<number, string[]>()
  private generations = new Map<number, string>()
  private sending = false
  private timer: ReturnType<typeof setTimeout> | undefined
  private retryDelay = 1000
  private paused = new Set<number>()
  private inFlight?: { articleId: number; settled: Promise<unknown> }
  private active = true
  private prefix?: string
  private storage?: Storage

  constructor(
    private save: (entry: PendingProgress) => Promise<T>,
    private stillAuthorized: () => boolean,
    private onSaved: (articleId: number, result: T) => void,
    persistence?: Persistence,
  ) {
    if (persistence && Number.isSafeInteger(persistence.ownerId) && persistence.ownerId! > 0) {
      this.prefix = `rsspal:progress:v1:${persistence.ownerId}:`
      try { this.storage = persistence.storage ?? localStorage } catch { /* memory fallback */ }
    }
  }

  private generation(articleId: number): string {
    try { return this.storage?.getItem(`${this.prefix}${articleId}:reset`) || this.generations.get(articleId) || '' }
    catch { return this.generations.get(articleId) || '' }
  }

  private restore() {
    if (!this.storage || !this.prefix) return
    const pending = new Map<number, PendingProgress>()
    const operations = new Map<number, string[]>()
    try {
      // Snapshot keys first. Concurrent enqueues have their own immutable keys,
      // so enumeration/acknowledgement never overwrites another tab's record.
      const keys: string[] = []
      for (let i = 0; i < this.storage.length; i++) {
        const key = this.storage.key(i)
        if (key?.startsWith(this.prefix) && key.includes(':pending:')) keys.push(key)
      }
      for (const key of keys) {
        let value: Snapshot
        const raw = this.storage.getItem(key)
        try { value = JSON.parse(raw || 'null') } catch { continue }
        const e = value?.entry
        if (!e || typeof value.revision !== 'string' || typeof value.generation !== 'string' ||
          !Number.isSafeInteger(e.articleId) || e.articleId <= 0 ||
          key !== `${this.prefix}${e.articleId}:pending:${value.revision}` ||
          !Number.isFinite(e.scrollPosition) || e.scrollPosition < 0 || e.scrollPosition > 1 ||
          typeof e.isCompleted !== 'boolean') continue
        if (value.generation !== this.generation(e.articleId)) {
          this.storage.removeItem(key)
          continue
        }
        const previous = pending.get(e.articleId)
        pending.set(e.articleId, previous ? { articleId: e.articleId,
          scrollPosition: Math.max(previous.scrollPosition, e.scrollPosition),
          isCompleted: previous.isCompleted || e.isCompleted } : e)
        operations.set(e.articleId, [...(operations.get(e.articleId) || []), key])
      }
      this.pending = pending
      this.operations = operations
    } catch { this.storage = undefined /* unavailable storage: preserve memory */ }
  }

  private persist(entry: PendingProgress, generation: string) {
    if (!this.prefix || !this.storage) return
    const revision = crypto.randomUUID()
    const key = `${this.prefix}${entry.articleId}:pending:${revision}`
    const snapshot: Snapshot = { revision, entry, generation }
    try {
      this.storage.setItem(key, JSON.stringify(snapshot))
      this.operations.set(entry.articleId, [...(this.operations.get(entry.articleId) || []), key])
    } catch { this.storage = undefined /* retain newer memory on quota/private mode */ }
  }

  // Web Locks serialize requests/reset across tabs. The local fallback also
  // orders detached page instances in browsers without Web Locks.
  private async locked<R>(articleId: number, action: () => Promise<R>): Promise<R> {
    if (!this.prefix) return action()
    const key = this.prefix + articleId
    if (typeof navigator !== 'undefined' && navigator.locks?.request) return await navigator.locks.request(key, action)
    const previous = localLocks.get(key)
    const result = previous ? previous.catch(() => {}).then(action) : action()
    localLocks.set(key, result)
    void result.finally(() => { if (localLocks.get(key) === result) localLocks.delete(key) }).catch(() => {})
    return result
  }

  enqueue(entry: PendingProgress) {
    if (!this.stillAuthorized() || this.paused.has(entry.articleId)) return
    const generation = this.generation(entry.articleId)
    this.restore()
    const previous = this.pending.get(entry.articleId)
    const next = previous ? { articleId: entry.articleId,
      scrollPosition: Math.max(previous.scrollPosition, entry.scrollPosition),
      isCompleted: previous.isCompleted || entry.isCompleted } : entry
    this.pending.set(entry.articleId, next)
    // Persist synchronously before any debounce/request, including page eviction.
    this.persist(next, generation)
  }

  schedule(delay = 500) {
    clearTimeout(this.timer)
    this.timer = setTimeout(() => { void this.flush() }, delay)
  }

  async flush() {
    clearTimeout(this.timer)
    if (this.sending || !this.stillAuthorized()) return
    this.restore()
    this.sending = true
    try {
      while (this.pending.size && this.stillAuthorized()) {
        const articleId = this.pending.keys().next().value!
        try {
          await this.locked(articleId, async () => {
            this.restore()
            const entry = this.pending.get(articleId)
            if (!entry || !this.stillAuthorized() || this.paused.has(articleId)) {
              this.pending.delete(articleId)
              return
            }
            const generation = this.generation(articleId)
            const sentKeys = this.operations.get(articleId) || []
            const request = this.save(entry)
            this.inFlight = { articleId, settled: request }
            const result = await request
            // Restore before acknowledging to discover later scrolls, but remove
            // only immutable records included in this particular request.
            this.restore()
            const unchangedGeneration = this.generation(articleId) === generation
            const laterKeys = this.operations.get(articleId) || []
            const sameSnapshot = this.storage
              ? unchangedGeneration && laterKeys.every(key => sentKeys.includes(key))
              : this.pending.get(articleId) === entry
            try { for (const key of sentKeys) this.storage?.removeItem(key) } catch { this.storage = undefined }
            if (sameSnapshot) this.pending.delete(articleId)
            this.restore()
            this.retryDelay = 1000
            if (sameSnapshot && this.active && !this.paused.has(articleId) && this.stillAuthorized()) this.onSaved(articleId, result)
          })
        } catch {
          if (this.pending.has(articleId)) this.schedule(this.retryDelay)
          this.retryDelay = Math.min(30000, this.retryDelay * 2)
          break
        } finally { this.inFlight = undefined }
      }
    } finally { this.sending = false }
  }

  async replace<R>(articleId: number, action: () => Promise<R>): Promise<R> {
    if (!this.stillAuthorized()) throw new Error('Reading progress account changed')
    this.paused.add(articleId)
    this.pending.delete(articleId)
    const generation = crypto.randomUUID()
    this.generations.set(articleId, generation)
    try { if (this.prefix) this.storage?.setItem(`${this.prefix}${articleId}:reset`, generation) }
    catch { this.storage = undefined }
    try {
      if (this.inFlight?.articleId === articleId) await this.inFlight.settled.catch(() => {})
      return await this.locked(articleId, async () => {
        if (!this.stillAuthorized()) throw new Error('Reading progress account changed')
        const result = await action()
        if (!this.stillAuthorized()) throw new Error('Reading progress account changed')
        return result
      })
    } finally { this.paused.delete(articleId) }
  }

  activate() { this.active = true; void this.flush() }
  detach() { this.active = false; void this.flush() }
}
