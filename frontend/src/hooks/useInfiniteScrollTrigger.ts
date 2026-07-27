import { useEffect, type RefObject } from 'react'

interface InfiniteScrollTriggerOptions {
  targetRef: RefObject<HTMLElement>
  enabled: boolean
  refreshKey: number
  rootMarginPx?: number
  onLoadMore: () => Promise<void>
}

export function useInfiniteScrollTrigger({
  targetRef,
  enabled,
  refreshKey,
  rootMarginPx = 200,
  onLoadMore,
}: InfiniteScrollTriggerOptions) {
  useEffect(() => {
    if (!enabled) return

    const target = targetRef.current
    if (!target) return

    let animationFrameId: number | null = null

    const checkTargetPosition = () => {
      animationFrameId = null
      if (target.getBoundingClientRect().top <= window.innerHeight + rootMarginPx) {
        void onLoadMore()
      }
    }

    const scheduleCheck = () => {
      if (animationFrameId !== null) return
      animationFrameId = window.requestAnimationFrame(checkTargetPosition)
    }

    window.addEventListener('scroll', scheduleCheck, { passive: true })
    document.addEventListener('scroll', scheduleCheck, { passive: true, capture: true })
    window.addEventListener('resize', scheduleCheck)
    scheduleCheck()

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          void onLoadMore()
        }
      },
      { rootMargin: `${rootMarginPx}px` },
    )
    observer.observe(target)

    return () => {
      observer.disconnect()
      window.removeEventListener('scroll', scheduleCheck)
      document.removeEventListener('scroll', scheduleCheck, true)
      window.removeEventListener('resize', scheduleCheck)
      if (animationFrameId !== null) {
        window.cancelAnimationFrame(animationFrameId)
      }
    }
  }, [enabled, onLoadMore, refreshKey, rootMarginPx, targetRef])
}
