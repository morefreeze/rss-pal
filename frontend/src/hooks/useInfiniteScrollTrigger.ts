import { useCallback, useEffect, useLayoutEffect, useRef, type RefObject } from 'react'

interface InfiniteScrollTriggerOptions {
  targetRef: RefObject<HTMLElement>
  enabled: boolean
  refreshKey: number
  activationKey: number
  rootMarginPx?: number
  onLoadMore: () => Promise<void>
}

export function useInfiniteScrollTrigger({
  targetRef,
  enabled,
  refreshKey,
  activationKey,
  rootMarginPx = 200,
  onLoadMore,
}: InfiniteScrollTriggerOptions) {
  const inFlightRef = useRef<{
    activationKey: number
    requestToken: symbol
  } | null>(null)
  const onLoadMoreRef = useRef(onLoadMore)

  useLayoutEffect(() => {
    onLoadMoreRef.current = onLoadMore
  }, [onLoadMore])

  const triggerLoad = useCallback(() => {
    if (!enabled || inFlightRef.current?.activationKey === activationKey) return

    const requestToken = Symbol('infinite-scroll-request')
    inFlightRef.current = { activationKey, requestToken }
    void Promise.resolve()
      .then(() => onLoadMoreRef.current())
      .catch(() => {})
      .finally(() => {
        if (inFlightRef.current?.requestToken === requestToken) {
          inFlightRef.current = null
        }
      })
  }, [activationKey, enabled])

  useEffect(() => {
    if (!enabled) return

    const target = targetRef.current
    if (!target) return

    let active = true
    let animationFrameId: number | null = null

    const checkTargetPosition = () => {
      animationFrameId = null
      if (!active) return
      if (target.getBoundingClientRect().top <= window.innerHeight + rootMarginPx) {
        triggerLoad()
      }
    }

    const scheduleCheck = () => {
      if (!active || animationFrameId !== null) return
      animationFrameId = window.requestAnimationFrame(checkTargetPosition)
    }

    window.addEventListener('scroll', scheduleCheck, { passive: true })
    document.addEventListener('scroll', scheduleCheck, { passive: true, capture: true })
    window.addEventListener('resize', scheduleCheck)
    scheduleCheck()

    const observer = new IntersectionObserver(
      (entries) => {
        if (!active) return
        if (entries.some((entry) => entry.isIntersecting)) {
          triggerLoad()
        }
      },
      { rootMargin: `${rootMarginPx}px` },
    )
    observer.observe(target)

    return () => {
      active = false
      observer.disconnect()
      window.removeEventListener('scroll', scheduleCheck)
      document.removeEventListener('scroll', scheduleCheck, true)
      window.removeEventListener('resize', scheduleCheck)
      if (animationFrameId !== null) {
        window.cancelAnimationFrame(animationFrameId)
      }
    }
  }, [enabled, refreshKey, rootMarginPx, targetRef, triggerLoad])
}
