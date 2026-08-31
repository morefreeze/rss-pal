import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  createExploreFeedback,
  deleteExploreFeedback,
  getExplore,
  recordExploreArticleEvent,
  type ExploreArticleListItem,
  type ExploreOrder,
  type ExploreSnapshotStatus,
  type ExploreSort,
} from '../api/client'

const DEFAULT_PAGE_SIZE = 20

interface ExploreFeedOptions {
  pageSize?: number
  initialSort?: ExploreSort
  initialOrder?: ExploreOrder
  initialTopic?: string
}

function mergeByID(
  current: ExploreArticleListItem[],
  incoming: ExploreArticleListItem[],
): ExploreArticleListItem[] {
  const seen = new Set(current.map(article => article.id))
  return [...current, ...incoming.filter(article => !seen.has(article.id))]
}

export function useExploreFeed({
  pageSize = DEFAULT_PAGE_SIZE,
  initialSort = 'published',
  initialOrder = 'desc',
  initialTopic,
}: ExploreFeedOptions = {}) {
  const [sort, setSort] = useState<ExploreSort>(initialSort)
  const [order, setOrder] = useState<ExploreOrder>(initialOrder)
  const [topic, setTopicState] = useState(initialTopic ?? '')
  const [baseArticles, setBaseArticles] = useState<ExploreArticleListItem[]>([])
  const [snapshot, setSnapshot] = useState<ExploreSnapshotStatus | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [offset, setOffset] = useState(0)
  const [generation, setGeneration] = useState(0)
  const [hiddenSources, setHiddenSources] = useState<Set<number>>(() => new Set())
  const [dampenedTopics, setDampenedTopics] = useState<Set<string>>(() => new Set())
  const [knownTopics, setKnownTopics] = useState<string[]>([])
  const requestGenerationRef = useRef(0)
  const exposedRef = useRef(new Set<number>())

  const requestPage = useCallback(async (nextOffset: number, reset: boolean, requestGeneration: number) => {
    if (reset) {
      setLoading(true)
      setLoadingMore(false)
      setError(null)
      setHasMore(false)
    } else {
      setLoadingMore(true)
    }

    try {
      const page = await getExplore({
        limit: pageSize,
        offset: nextOffset,
        sort,
        order,
        ...(topic ? { topic } : {}),
      })
      if (requestGeneration !== requestGenerationRef.current) return
      setSnapshot(page.snapshot)
      setBaseArticles(current => reset ? page.articles : mergeByID(current, page.articles))
      setKnownTopics(current => {
        const next = new Set(current)
        page.articles.forEach(article => { if (article.topic) next.add(article.topic) })
        return Array.from(next)
      })
      setHasMore(page.has_more)
      setOffset(nextOffset)
    } catch {
      if (requestGeneration !== requestGenerationRef.current) return
      setError('探索内容加载失败，请稍后重试')
      setHasMore(false)
    } finally {
      if (requestGeneration !== requestGenerationRef.current) return
      if (reset) setLoading(false)
      else setLoadingMore(false)
    }
  }, [order, pageSize, sort, topic])

  useEffect(() => {
    const nextGeneration = ++requestGenerationRef.current
    setGeneration(nextGeneration)
    setOffset(0)
    void requestPage(0, true, nextGeneration)
  }, [requestPage])

  const loadMore = useCallback(async () => {
    if (!hasMore || loading || loadingMore) return
    await requestPage(offset + pageSize, false, requestGenerationRef.current)
  }, [hasMore, loading, loadingMore, offset, pageSize, requestPage])

  const articles = useMemo(() => baseArticles.filter(article =>
    !hiddenSources.has(article.source_id) && !dampenedTopics.has(article.topic),
  ), [baseArticles, dampenedTopics, hiddenSources])

  const recordExposure = useCallback(async (articleID: number) => {
    if (exposedRef.current.has(articleID)) return
    exposedRef.current.add(articleID)
    try {
      await recordExploreArticleEvent(articleID, 'exposure')
    } catch {
      // Exposure is a low-weight signal and must never interrupt reading.
    }
  }, [])

  const recordClick = useCallback((articleID: number) => {
    void recordExploreArticleEvent(articleID, 'click').catch(() => {})
  }, [])

  const hideSource = useCallback(async (sourceID: number) => {
    setHiddenSources(current => new Set(current).add(sourceID))
    try {
      const feedback = await createExploreFeedback({ feedback_type: 'hide_source', source_id: sourceID })
      return async () => {
        await deleteExploreFeedback(feedback.id)
        setHiddenSources(current => {
          const next = new Set(current)
          next.delete(sourceID)
          return next
        })
      }
    } catch (requestError) {
      setHiddenSources(current => {
        const next = new Set(current)
        next.delete(sourceID)
        return next
      })
      throw requestError
    }
  }, [])

  const dampenTopic = useCallback(async (topicName: string) => {
    setDampenedTopics(current => new Set(current).add(topicName))
    try {
      const feedback = await createExploreFeedback({ feedback_type: 'dampen_topic', topic: topicName })
      return async () => {
        await deleteExploreFeedback(feedback.id)
        setDampenedTopics(current => {
          const next = new Set(current)
          next.delete(topicName)
          return next
        })
      }
    } catch (requestError) {
      setDampenedTopics(current => {
        const next = new Set(current)
        next.delete(topicName)
        return next
      })
      throw requestError
    }
  }, [])

  return {
    articles,
    snapshot,
    sort,
    order,
    topic,
    topics: knownTopics,
    loading,
    loadingMore,
    hasMore,
    error,
    requestGeneration: generation,
    setSort,
    setOrder,
    setTopic: (value: string) => setTopicState(value),
    loadMore,
    recordExposure,
    recordClick,
    hideSource,
    dampenTopic,
  }
}
