import { useCallback, useEffect } from 'react'
import { prefetchArticleDetail } from '../api/articleDetailCache'

const IDLE_LIMIT = 6
const DEFAULT_CONCURRENCY = 2

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

export function useArticleDetailPrefetch(ids: number[]) {
  const key = ids.slice(0, IDLE_LIMIT).join(',')

  useEffect(() => {
    const selected = key ? key.split(',').map(Number) : []
    if (selected.length === 0) return
    const start = () => { void prefetchArticleIDs(selected) }
    if ('requestIdleCallback' in window) {
      const idleID = window.requestIdleCallback(start, { timeout: 1000 })
      return () => window.cancelIdleCallback(idleID)
    }
    const timerID = window.setTimeout(start, 0)
    return () => window.clearTimeout(timerID)
  }, [key])

  return useCallback((id: number) => {
    void prefetchArticleDetail(id)
  }, [])
}
