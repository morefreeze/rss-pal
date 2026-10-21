export type PendingProgress = { articleId: number; scrollPosition: number; isCompleted: boolean }

// Keep unsaved high-water marks until the server acknowledges them. Each row
// carries its article identity, including requests finishing after navigation.
export class ProgressSaveQueue<T> {
  private pending = new Map<number, PendingProgress>()
  private sending = false
  private timer: ReturnType<typeof setTimeout> | undefined
  private retryDelay = 1000
  private paused = new Set<number>()
  private inFlight?: { articleId: number; settled: Promise<unknown> }
  private active = true
  private onSaved?: (articleId: number, result: T) => void

  constructor(
    private save: (entry: PendingProgress) => Promise<T>,
    private stillAuthorized: () => boolean,
    onSaved: (articleId: number, result: T) => void,
  ) { this.onSaved = onSaved }

  enqueue(entry: PendingProgress) {
    if (this.paused.has(entry.articleId)) return
    const previous = this.pending.get(entry.articleId)
    this.pending.set(entry.articleId, previous ? {
      articleId: entry.articleId,
      scrollPosition: Math.max(previous.scrollPosition, entry.scrollPosition),
      isCompleted: previous.isCompleted || entry.isCompleted,
    } : entry)
  }

  schedule(delay = 500) {
    clearTimeout(this.timer)
    this.timer = setTimeout(() => { void this.flush() }, delay)
  }

  async flush() {
    clearTimeout(this.timer)
    if (this.sending) return
    if (!this.stillAuthorized()) { this.pending.clear(); return }
    this.sending = true
    try {
      while (this.pending.size && this.stillAuthorized()) {
        const [articleId, entry] = this.pending.entries().next().value!
        try {
          const request = this.save(entry)
          this.inFlight = { articleId, settled: request }
          const result = await request
          // A newer scroll may arrive during the request. Acknowledge only
          // this snapshot, then send the newer one on the next iteration.
          if (this.pending.get(articleId) === entry) this.pending.delete(articleId)
          this.retryDelay = 1000
          if (this.active && !this.paused.has(articleId) && this.stillAuthorized()) this.onSaved?.(articleId, result)
        } catch {
          if (this.pending.has(articleId)) this.schedule(this.retryDelay)
          this.retryDelay = Math.min(30000, this.retryDelay * 2)
          break
        } finally { this.inFlight = undefined }
      }
    } finally { this.sending = false }
  }

  async replace<R>(articleId: number, action: () => Promise<R>): Promise<R> {
    this.paused.add(articleId)
    this.pending.delete(articleId)
    if (this.inFlight?.articleId === articleId) await this.inFlight.settled.catch(() => {})
    try { return await action() } finally { this.paused.delete(articleId) }
  }

  activate() { this.active = true }
  detach() { this.active = false; void this.flush() }
}
