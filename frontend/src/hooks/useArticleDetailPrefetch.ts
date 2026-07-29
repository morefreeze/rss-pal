import { useCallback, useEffect, useRef } from 'react'
import { prefetchArticleDetail } from '../api/articleDetailCache'

const IDLE_LIMIT = 6
const DEFAULT_CONCURRENCY = 2

// Viewport-driven (scroll) prefetch tuning. See useArticleDetailPrefetch's
// doc comment for the reasoning behind these numbers.
const VIEWPORT_DWELL_MS = 250
const VIEWPORT_ROOT_MARGIN_PX = 150
const VIEWPORT_CONCURRENCY = 2

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

// navigator.connection (the Network Information API) isn't in the default
// TS DOM lib. Narrow it locally instead of reaching for `any`.
interface NetworkInformationLike {
  saveData?: boolean
  effectiveType?: string
}

interface NavigatorWithConnection extends Navigator {
  connection?: NetworkInformationLike
}

function isSlowConnection(): boolean {
  const connection = (navigator as NavigatorWithConnection).connection
  if (!connection) return false
  if (connection.saveData === true) return true
  return connection.effectiveType === 'slow-2g' || connection.effectiveType === '2g'
}

export interface ArticleDetailPrefetchHandle {
  /** Hover/focus/touch-triggered prefetch — an explicit user intent signal. */
  promote: (id: number) => void
  /**
   * Ref callback cards register themselves with (merge into the card's
   * existing merged ref). Pass `null` to unregister. Backed by a single
   * shared IntersectionObserver — cards register/unregister an element
   * rather than each owning an observer instance.
   */
  registerCard: (id: number, el: Element | null) => void
}

/**
 * Prefetches article detail payloads so opening an article is instant.
 *
 * Two complementary triggers feed the same cache:
 *  - Idle-time prefetch of the first `ids` (capped at 6) shortly after mount.
 *  - Viewport-driven prefetch: as cards registered via `registerCard` stay
 *    intersecting the viewport for `VIEWPORT_DWELL_MS`, their detail is
 *    queued for prefetch. The dwell delay exists so fast scrolling through
 *    a long list doesn't fire a burst of requests for cards the user never
 *    actually reads — only cards that linger get prefetched.
 *
 * Both paths share the same bounded-concurrency queue idea (max 2 in
 * flight) and the same dedup set, so scrolling never saturates an
 * already-slow connection with more than a couple of concurrent detail
 * fetches, and a card already requested is never re-queued.
 *
 * `enabled` gates viewport-driven prefetching only (e.g. off in 网摘/clip
 * mode); the hover/focus/touch `promote` path always stays live since it's
 * an explicit user intent signal.
 */
export function useArticleDetailPrefetch(
  ids: number[],
  enabled = true,
): ArticleDetailPrefetchHandle {
  const key = ids.slice(0, IDLE_LIMIT).join(',')

  useEffect(() => {
    const selected = key ? key.split(',').map(Number) : []
    if (selected.length === 0) return
    const start = () => { void prefetchArticleIDs(selected) }
    if ('requestIdleCallback' in window) {
      const idleID = window.requestIdleCallback(start, { timeout: 1000 })
      return () => window.cancelIdleCallback(idleID)
    }
    const timerID = globalThis.setTimeout(start, 0)
    return () => globalThis.clearTimeout(timerID)
  }, [key])

  const promote = useCallback((id: number) => {
    void prefetchArticleDetail(id)
  }, [])

  // ----- viewport-driven (scroll) prefetch -----

  const enabledRef = useRef(enabled)
  useEffect(() => {
    enabledRef.current = enabled
  }, [enabled])

  const observerRef = useRef<IntersectionObserver | null>(null)
  const elementToIdRef = useRef(new Map<Element, number>())
  const idToElementRef = useRef(new Map<number, Element>())
  const dwellTimersRef = useRef(new Map<number, ReturnType<typeof setTimeout>>())
  const seenRef = useRef(new Set<number>())
  const queueRef = useRef<number[]>([])
  const activeCountRef = useRef(0)

  const clearDwellTimer = useCallback((id: number) => {
    const timer = dwellTimersRef.current.get(id)
    if (timer !== undefined) {
      clearTimeout(timer)
      dwellTimersRef.current.delete(id)
    }
  }, [])

  const pump = useCallback(() => {
    while (activeCountRef.current < VIEWPORT_CONCURRENCY && queueRef.current.length > 0) {
      const id = queueRef.current.shift()
      if (id === undefined) break
      activeCountRef.current += 1
      void prefetchArticleDetail(id).finally(() => {
        activeCountRef.current -= 1
        pump()
      })
    }
  }, [])

  const enqueue = useCallback((id: number) => {
    if (seenRef.current.has(id)) return
    seenRef.current.add(id)
    queueRef.current.push(id)
    pump()
  }, [pump])

  const handleIntersections = useCallback<IntersectionObserverCallback>((entries) => {
    for (const entry of entries) {
      const id = elementToIdRef.current.get(entry.target)
      if (id === undefined) continue

      if (!entry.isIntersecting) {
        clearDwellTimer(id)
        continue
      }

      if (!enabledRef.current) continue
      if (seenRef.current.has(id)) continue
      if (dwellTimersRef.current.has(id)) continue
      if (isSlowConnection()) continue

      const timer = setTimeout(() => {
        dwellTimersRef.current.delete(id)
        enqueue(id)
      }, VIEWPORT_DWELL_MS)
      dwellTimersRef.current.set(id, timer)
    }
  }, [clearDwellTimer, enqueue])

  const ensureObserver = useCallback((): IntersectionObserver | null => {
    if (observerRef.current) return observerRef.current
    if (typeof IntersectionObserver === 'undefined') return null
    const observer = new IntersectionObserver(handleIntersections, {
      rootMargin: `${VIEWPORT_ROOT_MARGIN_PX}px`,
    })
    observerRef.current = observer
    return observer
  }, [handleIntersections])

  const registerCard = useCallback((id: number, el: Element | null) => {
    const prevEl = idToElementRef.current.get(id)
    if (prevEl === el) return

    if (prevEl) {
      observerRef.current?.unobserve(prevEl)
      elementToIdRef.current.delete(prevEl)
      idToElementRef.current.delete(id)
    }
    clearDwellTimer(id)

    if (!el) return

    const observer = ensureObserver()
    if (!observer) return
    idToElementRef.current.set(id, el)
    elementToIdRef.current.set(el, id)
    observer.observe(el)
  }, [ensureObserver, clearDwellTimer])

  useEffect(() => {
    return () => {
      observerRef.current?.disconnect()
      observerRef.current = null
      for (const timer of dwellTimersRef.current.values()) clearTimeout(timer)
      dwellTimersRef.current.clear()
      elementToIdRef.current.clear()
      idToElementRef.current.clear()
    }
  }, [])

  return { promote, registerCard }
}
