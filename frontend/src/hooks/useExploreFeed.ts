import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  createExploreFeedback,
  deleteExploreFeedback,
  getExplore,
  getUser,
  recordExploreArticleEvent,
  type ExploreArticleListItem,
  type ExploreOrder,
  type ExploreSnapshotStatus,
  type ExploreSort,
} from '../api/client'

const DEFAULT_PAGE_SIZE = 20
const EXPOSURE_SESSION_KEY_PREFIX = 'exploreReportedExposures'
const MAX_AUTOMATIC_EMPTY_PAGE_LOADS = 5

function exposureSessionKey(): string {
  try {
    const userID = getUser()?.id
    return `${EXPOSURE_SESSION_KEY_PREFIX}:${Number.isInteger(userID) ? userID : 'unknown'}`
  } catch {
    return `${EXPOSURE_SESSION_KEY_PREFIX}:unknown`
  }
}

function reportedExposures(key: string): Set<number> {
  try {
    const values = JSON.parse(sessionStorage.getItem(key) || '[]')
    return new Set(Array.isArray(values) ? values.filter(Number.isInteger) : [])
  } catch {
    return new Set()
  }
}

function persistReportedExposures(key: string, values: Set<number>) {
  try {
    sessionStorage.setItem(key, JSON.stringify(Array.from(values)))
  } catch {
    // Session storage can be unavailable or full; in-memory de-noising remains.
  }
}

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
  const merged = [...current]
  for (const article of incoming) {
    if (seen.has(article.id)) continue
    seen.add(article.id)
    merged.push(article)
  }
  return merged
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
  const requestInFlightGenerationRef = useRef<number | null>(null)
  const automaticEmptyLoadsRef = useRef(0)
  const exposureKeyRef = useRef(exposureSessionKey())
  const exposedRef = useRef(reportedExposures(exposureKeyRef.current))
  const exposureInFlightRef = useRef(new Map<number, Promise<boolean>>())
  const [automaticLoadLimitReached, setAutomaticLoadLimitReached] = useState(false)

  const requestPage = useCallback(async (nextOffset: number, reset: boolean, requestGeneration: number) => {
    requestInFlightGenerationRef.current = requestGeneration
    if (reset) {
      setLoading(true)
      setLoadingMore(false)
      setError(null)
      setHasMore(false)
    } else {
      setLoadingMore(true)
      setError(null)
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
      setBaseArticles(current => reset ? mergeByID([], page.articles) : mergeByID(current, page.articles))
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
    } finally {
      if (requestGeneration === requestGenerationRef.current) {
        requestInFlightGenerationRef.current = null
        if (reset) setLoading(false)
        else setLoadingMore(false)
      }
    }
  }, [order, pageSize, sort, topic])

  const reload = useCallback(async () => {
    const nextGeneration = ++requestGenerationRef.current
    automaticEmptyLoadsRef.current = 0
    setAutomaticLoadLimitReached(false)
    setHiddenSources(new Set())
    setDampenedTopics(new Set())
    setGeneration(nextGeneration)
    setOffset(0)
    await requestPage(0, true, nextGeneration)
  }, [requestPage])

  useEffect(() => {
    void reload()
  }, [reload])

  const loadMore = useCallback(async () => {
    if (!hasMore || loading || loadingMore || requestInFlightGenerationRef.current === requestGenerationRef.current) return
    await requestPage(offset + pageSize, false, requestGenerationRef.current)
  }, [hasMore, loading, loadingMore, offset, pageSize, requestPage])

  const continueLoading = useCallback(async () => {
    automaticEmptyLoadsRef.current = 0
    setAutomaticLoadLimitReached(false)
    await loadMore()
  }, [loadMore])

  const articles = useMemo(() => baseArticles.filter(article =>
    !hiddenSources.has(article.source_id) && !dampenedTopics.has(article.topic),
  ), [baseArticles, dampenedTopics, hiddenSources])

  useEffect(() => {
    if (articles.length > 0 || !hasMore || loading || loadingMore || error) return
    if (requestInFlightGenerationRef.current === requestGenerationRef.current) return
    if (automaticEmptyLoadsRef.current >= MAX_AUTOMATIC_EMPTY_PAGE_LOADS) {
      setAutomaticLoadLimitReached(true)
      return
    }
    automaticEmptyLoadsRef.current += 1
    void loadMore()
  }, [articles.length, error, hasMore, loadMore, loading, loadingMore, offset])

  const recordExposure = useCallback((articleID: number): Promise<boolean> => {
    if (exposedRef.current.has(articleID)) return Promise.resolve(true)
    const pending = exposureInFlightRef.current.get(articleID)
    if (pending) return pending
    const request = recordExploreArticleEvent(articleID, 'exposure')
      .then(() => {
        exposedRef.current.add(articleID)
        persistReportedExposures(exposureKeyRef.current, exposedRef.current)
        return true
      })
      .catch(() => {
        // Exposure is a low-weight signal and must never interrupt reading.
        return false
      })
      .finally(() => {
        exposureInFlightRef.current.delete(articleID)
      })
    exposureInFlightRef.current.set(articleID, request)
    return request
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
    automaticLoadLimitReached,
    error,
    requestGeneration: generation,
    setSort,
    setOrder,
    setTopic: (value: string) => setTopicState(value),
    loadMore,
    continueLoading,
    reload,
    recordExposure,
    recordClick,
    hideSource,
    dampenTopic,
  }
}
