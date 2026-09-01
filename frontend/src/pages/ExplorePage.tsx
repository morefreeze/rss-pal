import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import {
  clearExploreNegativeFeedback,
  replaceExploreInterests,
  type ExploreArticleListItem,
  type ExploreOrder,
  type ExploreSort,
} from '../api/client'
import ExploreArticleCard from '../components/ExploreArticleCard'
import ExploreSourceDrawer from '../components/ExploreSourceDrawer'
import { useExploreFeed } from '../hooks/useExploreFeed'
import { useInfiniteScrollTrigger } from '../hooks/useInfiniteScrollTrigger'
import { toast } from '../utils/toast'

const PAGE_SIZE = 20
const PREFETCH_OFFSET = 5

const EXPLORE_INTERESTS = [
  ['programming', '编程'],
  ['security', '安全'],
  ['ai', 'AI'],
  ['technology', '科技'],
  ['web-development', 'Web 开发'],
  ['self-hosted', '自托管'],
  ['business', '商业'],
  ['health', '健康'],
  ['news', '新闻'],
  ['podcast', '播客'],
  ['youtube', '视频'],
  ['enterprise', '企业服务'],
  ['blog', '博客'],
  ['cn_tech', '中文科技'],
  ['chinese-independent', '中文独立博客'],
  ['recently-added', '新近收录'],
  ['ai_eng', 'AI 工程'],
] as const

type UndoFeedback = () => Promise<void>

export default function ExplorePage() {
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const initialSort: ExploreSort = searchParams.get('sort') === 'captured' ? 'captured' : 'published'
  const initialOrder: ExploreOrder = searchParams.get('order') === 'asc' ? 'asc' : 'desc'
  const initialTopic = searchParams.get('topic') || undefined
  const feed = useExploreFeed({ pageSize: PAGE_SIZE, initialSort, initialOrder, initialTopic })
  const loadMoreRef = useRef<HTMLDivElement>(null)
  const [feedbackError, setFeedbackError] = useState<string | null>(null)
  const [feedbackUndos, setFeedbackUndos] = useState<UndoFeedback[]>([])
  const [feedbackClearing, setFeedbackClearing] = useState(false)
  const [selectedInterests, setSelectedInterests] = useState<Set<string>>(() => new Set())
  const interestsInitializedRef = useRef(false)
  const [interestSaving, setInterestSaving] = useState(false)
  const [interestError, setInterestError] = useState<string | null>(null)
  const [interestSaved, setInterestSaved] = useState(false)

  useEffect(() => {
    if (feed.interests === null || interestsInitializedRef.current) return
    interestsInitializedRef.current = true
    setSelectedInterests(new Set(feed.interests))
    setInterestError(null)
  }, [feed.interests])

  useInfiniteScrollTrigger({
    targetRef: loadMoreRef,
    enabled: feed.hasMore && !feed.loading && !feed.loadingMore,
    refreshKey: feed.articles.length,
    activationKey: feed.requestGeneration,
    rootMarginPx: 200,
    onLoadMore: feed.loadMore,
  })

  const updateQuery = useCallback((changes: Partial<{ sort: ExploreSort; order: ExploreOrder; topic: string }>) => {
    const next = new URLSearchParams(searchParams)
    if (changes.sort !== undefined) next.set('sort', changes.sort)
    if (changes.order !== undefined) next.set('order', changes.order)
    if (changes.topic !== undefined) {
      if (changes.topic) next.set('topic', changes.topic)
      else next.delete('topic')
    }
    setSearchParams(next, { replace: true })
  }, [searchParams, setSearchParams])

  const pickSort = (nextSort: ExploreSort) => {
    if (nextSort === feed.sort) {
      const nextOrder: ExploreOrder = feed.order === 'desc' ? 'asc' : 'desc'
      feed.setOrder(nextOrder)
      updateQuery({ sort: nextSort, order: nextOrder })
      return
    }
    feed.setSort(nextSort)
    updateQuery({ sort: nextSort, order: feed.order })
  }

  const pickTopic = (nextTopic: string) => {
    feed.setTopic(nextTopic)
    updateQuery({ topic: nextTopic })
  }

  const openArticle = (article: ExploreArticleListItem) => {
    feed.recordClick(article.id)
    const from = `${location.pathname}${location.search}`
    navigate(`/explore/articles/${article.id}`, { state: { from, articlePreview: article } })
  }

  const submitFeedback = async (kind: 'source' | 'topic', article: ExploreArticleListItem) => {
    setFeedbackError(null)
    try {
      const undo = kind === 'source'
        ? await feed.hideSource(article.source_id)
        : await feed.dampenTopic(article.topic)
      setFeedbackUndos(current => [...current, undo])
      toast.info(kind === 'source' ? '已隐藏这个来源' : '已减少这类内容', {
        action: {
          label: '撤销',
          onClick: () => {
            void undoFeedback(undo)
          },
        },
      })
    } catch {
      setFeedbackError('反馈失败，已恢复')
      toast.error('反馈失败，已恢复')
    }
  }

  const undoFeedback = async (undo: UndoFeedback) => {
    setFeedbackError(null)
    try {
      await undo()
      setFeedbackUndos(current => current.filter(item => item !== undo))
      return true
    } catch {
      setFeedbackError('撤销失败，请稍后重试')
      toast.error('撤销失败，请稍后重试')
      return false
    }
  }

  const clearSessionFeedback = async () => {
    for (const undo of [...feedbackUndos].reverse()) {
      if (!await undoFeedback(undo)) break
    }
  }

  const clearNegativeFeedback = async () => {
    if (feedbackClearing) return
    setFeedbackClearing(true)
    setFeedbackError(null)
    try {
      await clearExploreNegativeFeedback()
      setFeedbackUndos([])
      await feed.reload()
    } catch {
      setFeedbackError('清除反馈失败，请重试')
    } finally {
      setFeedbackClearing(false)
    }
  }

  const toggleInterest = (value: string) => {
    setInterestSaved(false)
    setInterestError(null)
    setSelectedInterests(current => {
      const next = new Set(current)
      if (next.has(value)) next.delete(value)
      else next.add(value)
      return next
    })
  }

  const saveInterests = async () => {
    if (feed.interests === null || selectedInterests.size === 0 || interestSaving) return
    setInterestSaving(true)
    setInterestError(null)
    setInterestSaved(false)
    try {
      const ordered = EXPLORE_INTERESTS
        .map(([value]) => value)
        .filter(value => selectedInterests.has(value))
      await replaceExploreInterests(ordered)
      setInterestSaved(true)
    } catch {
      setInterestError('兴趣保存失败，请重试')
    } finally {
      setInterestSaving(false)
    }
  }

  const prefetchIndex = feed.hasMore && feed.articles.length > 0
    ? Math.max(0, feed.articles.length - PREFETCH_OFFSET)
    : -1
  const sortArrow = feed.order === 'asc' ? '↑' : '↓'
  const topics = useMemo(() => {
    const items = new Set(feed.topics)
    if (feed.topic) items.add(feed.topic)
    return Array.from(items)
  }, [feed.topic, feed.topics])
  const latestUndo: UndoFeedback | undefined = feedbackUndos.length > 0
    ? feedbackUndos[feedbackUndos.length - 1]
    : undefined

  return (
    <div className="explore-page">
      <header className="explore-header">
        <div>
          <h1>探索</h1>
          <p>从你的订阅出发，发现持续更新的新来源</p>
        </div>
        <div className="explore-toolbar" aria-label="探索排序与筛选">
          <label>
            <span className="sr-only">主题筛选</span>
            <select className="toolbar-control" aria-label="主题筛选" value={feed.topic} onChange={event => pickTopic(event.target.value)}>
              <option value="">全部主题</option>
              {topics.map(topic => <option key={topic} value={topic}>{topic}</option>)}
            </select>
          </label>
          <div className="explore-sort-buttons" aria-label="排序方式">
            {(['published', 'captured'] as const).map(sort => (
              <button
                key={sort}
                type="button"
                className={feed.sort === sort ? '' : 'btn-ghost'}
                aria-pressed={feed.sort === sort}
                onClick={() => pickSort(sort)}
              >
                {sort === 'published' ? '发布' : '抓取'}{feed.sort === sort ? ` ${sortArrow}` : ''}
              </button>
            ))}
          </div>
        </div>
      </header>

      {feed.snapshot?.id === 0 && (
        <section className="explore-notice explore-notice--cold">
          <strong>正在为你发现第一批优质内容</strong>
          <span>先选择几个兴趣，系统会用稳定更新的公开来源生成候选内容。</span>
          <div className="explore-interest-picker" role="group" aria-label="选择探索兴趣">
            {EXPLORE_INTERESTS.map(([value, label]) => (
              <button
                key={value}
                type="button"
                className={selectedInterests.has(value) ? '' : 'secondary'}
                aria-pressed={selectedInterests.has(value)}
                onClick={() => toggleInterest(value)}
              >{label}</button>
            ))}
          </div>
          <div className="explore-interest-actions">
            <button type="button" disabled={feed.interests === null || interestSaving || selectedInterests.size === 0} onClick={() => void saveInterests()}>
              {interestSaving ? '保存中…' : '保存兴趣'}
            </button>
            <Link to="/interests">查看完整兴趣页</Link>
          </div>
          {interestError && <div role="alert" className="explore-inline-error">{interestError}</div>}
          {interestSaved && <div role="status" className="text-muted">兴趣已保存，下一批推荐会优先使用这些主题</div>}
        </section>
      )}
      {feed.snapshot?.generating && <div className="explore-notice">推荐正在后台优化，当前内容可以继续阅读</div>}
      {(feed.snapshot?.using_fallback || feed.snapshot?.refresh_failed) && (
        <div className="explore-notice explore-notice--warning">
          最近一次更新失败，正在沿用上一批可用内容
          {feed.snapshot.completed_at ? ` · 上次更新 ${new Date(feed.snapshot.completed_at).toLocaleString('zh-CN')}` : ''}
        </div>
      )}
      {feedbackError && <div role="alert" className="explore-inline-error">{feedbackError}</div>}
      {feed.error && <div role="alert" className="explore-inline-error">{feed.error}</div>}

      <main className="explore-stream" aria-busy={feed.loading}>
        {feed.loading && feed.articles.length === 0 ? (
          <div className="card text-muted">正在加载探索内容…</div>
        ) : feed.articles.length === 0 && feed.topic ? (
          <div className="card explore-empty-state">
            <strong>当前主题没有候选文章</strong>
            <span>可能被筛选或反馈隐藏了。</span>
            {feed.error && <button type="button" className="secondary" onClick={() => void feed.reload()}>重试加载</button>}
            <button type="button" className="secondary" aria-label="清除主题筛选" onClick={() => pickTopic('')}>清除筛选</button>
          </div>
        ) : feed.articles.length === 0 && !feed.topic ? (
          <div className="card explore-empty-state">
            <strong>{latestUndo ? '反馈已隐藏当前候选文章' : '暂时没有候选文章'}</strong>
            <span>{latestUndo ? '可以撤销最近一次反馈，恢复原来的文章位置。' : '新来源验证后会自动出现在这里。'}</span>
            {latestUndo && <button type="button" className="secondary" aria-label="撤销最近反馈" onClick={() => void undoFeedback(latestUndo)}>撤销最近反馈</button>}
            {latestUndo && feedbackUndos.length > 1 && (
              <button type="button" className="secondary" aria-label={`清除本次反馈（${feedbackUndos.length}）`} onClick={() => void clearSessionFeedback()}>
                清除本次反馈（{feedbackUndos.length}）
              </button>
            )}
            <button type="button" className="secondary" aria-label="清除隐藏/少推荐反馈" disabled={feedbackClearing} onClick={() => void clearNegativeFeedback()}>
              {feedbackClearing ? '清除中…' : '清除隐藏/少推荐反馈'}
            </button>
            {feed.error && (
              <button type="button" className="secondary" onClick={() => void (feed.hasMore ? feed.continueLoading() : feed.reload())}>重试加载</button>
            )}
            {!feed.error && feed.hasMore && feed.automaticLoadLimitReached && (
              <button type="button" className="secondary" onClick={() => void feed.continueLoading()}>继续加载</button>
            )}
          </div>
        ) : (
          <>
            {feed.articles.map((article, index) => (
              <ExploreArticleCard
                key={article.id}
                article={article}
                sort={feed.sort}
                loadMoreRef={index === prefetchIndex ? loadMoreRef : undefined}
                onOpen={openArticle}
                onExposure={feed.recordExposure}
                onHideSource={() => void submitFeedback('source', article)}
                onDampenTopic={() => void submitFeedback('topic', article)}
              />
            ))}
            {feed.hasMore ? (
              <div className="explore-load-more">
                {feed.loadingMore
                  ? <span>加载中…</span>
                  : <button type="button" className="secondary" onClick={() => void feed.loadMore()}>加载更多</button>}
              </div>
            ) : feed.articles.length > 0 ? (
              <div className="explore-end">— 已加载全部候选文章 —</div>
            ) : null}
          </>
        )}
      </main>

      <ExploreSourceDrawer />
    </div>
  )
}
