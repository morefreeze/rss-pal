import { useEffect, useRef } from 'react'
import { postEvent } from '../api/client'

const EXPOSURE_THRESHOLD_MS = 10_000
const VISIBILITY_THRESHOLD = 0.5

// sessionStorage key for already-reported (article_id, event_type) pairs
// to avoid duplicate exposure events within the same browser session.
const SESSION_KEY = 'reportedEvents'

function alreadyReported(articleId: number, type: 'exposure' | 'click'): boolean {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY)
    const set: string[] = raw ? JSON.parse(raw) : []
    return set.includes(`${type}:${articleId}`)
  } catch {
    return false
  }
}

export function markReported(articleId: number, type: 'exposure' | 'click'): void {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY)
    const set: string[] = raw ? JSON.parse(raw) : []
    const key = `${type}:${articleId}`
    if (!set.includes(key)) {
      set.push(key)
      sessionStorage.setItem(SESSION_KEY, JSON.stringify(set))
    }
  } catch {
    // ignore quota errors
  }
}

/**
 * useExposureTracking attaches an IntersectionObserver to a ref'd element
 * and fires a single 'exposure' event after the element has been visibly
 * intersecting (≥0.5) continuously for 10 seconds in this browser session.
 */
export function useExposureTracking(articleId: number) {
  const ref = useRef<HTMLDivElement | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (alreadyReported(articleId, 'exposure')) return

    const observer = new IntersectionObserver(
      entries => {
        const entry = entries[0]
        if (!entry) return
        if (entry.isIntersecting && entry.intersectionRatio >= VISIBILITY_THRESHOLD) {
          if (timerRef.current) return
          timerRef.current = setTimeout(() => {
            markReported(articleId, 'exposure')
            postEvent(articleId, 'exposure').catch(() => {})
            observer.disconnect()
          }, EXPOSURE_THRESHOLD_MS)
        } else {
          if (timerRef.current) {
            clearTimeout(timerRef.current)
            timerRef.current = null
          }
        }
      },
      { threshold: [0, VISIBILITY_THRESHOLD, 1] }
    )

    observer.observe(el)
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current)
        timerRef.current = null
      }
      observer.disconnect()
    }
  }, [articleId])

  return ref
}

/** Fires a click event before navigation; idempotent per session. */
export function reportClick(articleId: number): void {
  if (alreadyReported(articleId, 'click')) return
  markReported(articleId, 'click')
  postEvent(articleId, 'click').catch(() => {})
}
