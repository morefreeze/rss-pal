import ExploreArticleActions from '../components/ExploreArticleActions'
import { useExploreArticleActions } from '../hooks/useExploreArticleActions'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import {
  getExploreArticle,
  fetchExploreArticleContent,
  recordExploreArticleEvent,
  subscribeExploreSource,
  type ExploreArticleDetail,
  type ExploreArticleListItem,
} from '../api/client'
import { CodeWrapContext } from '../components/CodeWrapContext'
import MarkdownArticle from '../components/MarkdownArticle'
import ReaderSettingsPanel from '../components/ReaderSettingsPanel'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useReaderSettings } from '../hooks/useReaderSettings'
import { resolveDetailPreviewTitle } from '../utils/pageTitle'
import { computeViewportProgress } from '../utils/readingProgress'

type ExploreArticleLocationState = {
  from?: unknown
  articlePreview?: ExploreArticleListItem
}

function safeExploreReturnPath(value: unknown): string {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) {
    return '/explore'
  }
  try {
    const parsed = new URL(value, window.location.origin)
    if (parsed.origin !== window.location.origin || parsed.pathname !== '/explore') return '/explore'
    return `${parsed.pathname}${parsed.search}${parsed.hash}`
  } catch {
    return '/explore'
  }
}

function formatDate(value: string | null): string {
  if (!value) return '发布时间未知'
  return new Date(value).toLocaleString('zh-CN')
}

export default function ExploreArticlePage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const location = useLocation()
  const reader = useReaderSettings()
  const articleID = Number(id)
  const locationState = location.state as ExploreArticleLocationState | null
  const preview = locationState?.articlePreview?.id === articleID
    ? locationState.articlePreview
    : undefined
  const returnPath = useMemo(() => safeExploreReturnPath(locationState?.from), [locationState?.from])
  const [article, setArticle] = useState<ExploreArticleDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [contentLoading, setContentLoading] = useState(false)
  const [contentError, setContentError] = useState('')
  const contentGenerationRef = useRef(0)
  const contentRequestRef = useRef<AbortController | null>(null)
  const currentArticleIDRef = useRef(articleID)
  currentArticleIDRef.current = articleID
  const [subscribed, setSubscribed] = useState(Boolean(preview?.is_subscribed))
  const [subscribing, setSubscribing] = useState(false)
  const [subscribeError, setSubscribeError] = useState('')
  const loadGenerationRef = useRef(0)
  const subscribeGenerationRef = useRef(0)
  const subscribingRef = useRef(false)
  const clickedArticleIDsRef = useRef(new Set<number>())
  const completedReadInflightIDsRef = useRef(new Set<number>())
  const completedReadSucceededIDsRef = useRef(new Set<number>())

  const previewTitle = resolveDetailPreviewTitle(articleID, locationState)
  useDocumentTitle(
    article?.title
      ?? previewTitle
      ?? (Number.isFinite(articleID) && articleID > 0 ? `探索文章 ${articleID}` : '探索文章'),
  )

  const loadArticle = useCallback(async () => {
    const generation = ++loadGenerationRef.current
    if (!Number.isInteger(articleID) || articleID <= 0) {
      setArticle(null)
      setLoading(false)
      setLoadError('探索文章不存在或已失效')
      return
    }

    setLoading(true)
    setLoadError('')
    setSubscribeError('')
    try {
      const loaded = await getExploreArticle(articleID)
      if (generation !== loadGenerationRef.current) return
      setArticle(loaded)
      setSubscribed(loaded.is_subscribed)
    } catch (error: any) {
      if (generation !== loadGenerationRef.current) return
      const status = error?.response?.status
      setArticle(null)
      if (status === 403) setLoadError('无权访问这篇探索文章')
      else if (status === 404) setLoadError('探索文章不存在或已失效')
      else setLoadError('探索文章加载失败，请稍后重试')
    } finally {
      if (generation === loadGenerationRef.current) setLoading(false)
    }
  }, [articleID, preview?.is_subscribed])

  useEffect(() => {
    subscribeGenerationRef.current += 1
    subscribingRef.current = false
    setSubscribing(false)
    setArticle(null)
    setContentLoading(false)
    setContentError('')
    setSubscribed(Boolean(preview?.is_subscribed))
    // React Router preserves window scroll across client-side navigation.
    // A list's scroll offset must never become this article's read progress.
    window.scrollTo(0, 0)
    void loadArticle()
    return () => {
      loadGenerationRef.current += 1
      contentGenerationRef.current += 1
      contentRequestRef.current?.abort()
      contentRequestRef.current = null
      subscribeGenerationRef.current += 1
      subscribingRef.current = false
    }
  }, [loadArticle, preview?.is_subscribed])

  const fetchOriginal = useCallback(async () => {
    if (contentRequestRef.current) return
    const controller = new AbortController()
    const generation = ++contentGenerationRef.current
    contentRequestRef.current = controller
    const isCurrent = () => !controller.signal.aborted && currentArticleIDRef.current === articleID
      && contentGenerationRef.current === generation
    setContentLoading(true)
    setContentError('')
    try {
      const loaded = await fetchExploreArticleContent(articleID, controller.signal)
      if (isCurrent() && loaded.id === articleID) {
        // Preserve local save/skip state while replacing only reading content.
        setArticle(current => current?.id === articleID ? { ...current,
          content: loaded.content, content_status: loaded.content_status,
          content_error: loaded.content_error, content_retry_at: loaded.content_retry_at,
        } : current)
      }
    } catch (error: any) {
      if (isCurrent()) setContentError(error?.response?.data?.error || '获取原文失败，请检查网络后重试。')
    } finally {
      if (isCurrent()) setContentLoading(false)
      if (contentRequestRef.current === controller) contentRequestRef.current = null
    }
  }, [articleID])

  useEffect(() => {
    if (article?.id === articleID && article.content_status === 'feed') void fetchOriginal()
  }, [article?.id, article?.content_status, articleID, fetchOriginal])

  useEffect(() => {
    if (article?.content_status !== 'fetching') return
    let canceled = false
    let inFlight = false
    const generation = contentGenerationRef.current
    const poll = async () => {
      if (inFlight || document.hidden) return
      inFlight = true
      try {
        const loaded = await getExploreArticle(articleID)
        if (canceled || currentArticleIDRef.current !== articleID || contentGenerationRef.current !== generation) return
        setContentError('')
        setArticle(current => current?.id === articleID ? { ...current,
          content: loaded.content, content_status: loaded.content_status,
          content_error: loaded.content_error, content_retry_at: loaded.content_retry_at,
        } : current)
      } catch { /* retain preview and retry the shared task status */ }
      finally { inFlight = false }
    }
    const timer = setInterval(() => { void poll() }, 3000)
    return () => { canceled = true; clearInterval(timer) }
  }, [article?.content_status, articleID])

  // ExplorePage records click before it navigates. A matching route preview is
  // the hand-off marker, so only direct/deep-link entries need a detail click.
  useEffect(() => {
    if (!article || preview || clickedArticleIDsRef.current.has(article.id)) return
    clickedArticleIDsRef.current.add(article.id)
    void recordExploreArticleEvent(article.id, 'click').catch(() => {})
  }, [article, preview])

  useEffect(() => {
    if (!article || contentLoading || (article.content_status && article.content_status !== 'fetched')) return
    const articleIDAtMount = article.id
    let disposed = false
    let animationFrameID: number | null = null

    const recordCompletion = async () => {
      const inflight = completedReadInflightIDsRef.current
      const succeeded = completedReadSucceededIDsRef.current
      if (inflight.has(articleIDAtMount) || succeeded.has(articleIDAtMount)) return
      inflight.add(articleIDAtMount)
      try {
        await recordExploreArticleEvent(articleIDAtMount, 'completed_read')
        succeeded.add(articleIDAtMount)
      } catch {
        // A later scroll, resize or visibility interaction may retry.
      } finally {
        inflight.delete(articleIDAtMount)
      }
    }

    const measureCompletion = () => {
      const scrollHeight = document.documentElement.scrollHeight
      const viewportHeight = window.innerHeight
      const isFullyVisible = scrollHeight > 0 && viewportHeight > 0 && scrollHeight <= viewportHeight
      const progress = computeViewportProgress(
        window.scrollY,
        scrollHeight,
        viewportHeight,
      )
      if (!isFullyVisible && progress < 0.9) return
      void recordCompletion()
    }

    const scheduleMeasurement = (afterLayoutSettles = false) => {
      if (disposed || animationFrameID !== null) return
      animationFrameID = window.requestAnimationFrame(() => {
        animationFrameID = null
        if (disposed) return
        if (afterLayoutSettles) {
          animationFrameID = window.requestAnimationFrame(() => {
            animationFrameID = null
            if (!disposed) measureCompletion()
          })
          return
        }
        measureCompletion()
      })
    }

    const onViewportInteraction = () => scheduleMeasurement()
    const onVisibilityChange = () => {
      if (!document.hidden) scheduleMeasurement()
    }
    const resizeObserver = new ResizeObserver(onViewportInteraction)
    resizeObserver.observe(document.documentElement)
    window.addEventListener('scroll', onViewportInteraction, { passive: true })
    window.addEventListener('resize', onViewportInteraction)
    document.addEventListener('visibilitychange', onVisibilityChange)
    scheduleMeasurement(true)

    return () => {
      disposed = true
      if (animationFrameID !== null) window.cancelAnimationFrame(animationFrameID)
      resizeObserver.disconnect()
      window.removeEventListener('scroll', onViewportInteraction)
      window.removeEventListener('resize', onViewportInteraction)
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [article, contentLoading])

  const handleSubscribe = async () => {
    if (!article || subscribed || subscribingRef.current) return
    const generation = ++subscribeGenerationRef.current
    subscribingRef.current = true
    setSubscribing(true)
    setSubscribeError('')
    try {
      await subscribeExploreSource(article.source_id)
      if (generation !== subscribeGenerationRef.current) return
      setSubscribed(true)
    } catch {
      if (generation !== subscribeGenerationRef.current) return
      setSubscribeError('订阅失败，请稍后重试')
    } finally {
      if (generation === subscribeGenerationRef.current) {
        subscribingRef.current = false
        setSubscribing(false)
      }
    }
  }

  const goBack = () => navigate(returnPath)
  const actions = useExploreArticleActions(articleID, article?.saved, goBack, article?.skipped)

  if (loading) {
    return (
      <div className="reading-layout" aria-busy="true">
        <div className="reading-toolbar">
          <button type="button" className="reading-exit" aria-label="返回探索" onClick={goBack}>← 返回探索</button>
        </div>
        <div className="card" role="status">正在加载探索文章…</div>
      </div>
    )
  }

  if (loadError || !article) {
    return (
      <div className="reading-layout">
        <div className="reading-toolbar">
          <button type="button" className="reading-exit" aria-label="返回探索" onClick={goBack}>← 返回探索</button>
        </div>
        <div className="card" role="alert">
          <p>{loadError || '探索文章不存在或已失效'}</p>
          <button type="button" onClick={() => void loadArticle()}>重试</button>
        </div>
      </div>
    )
  }

  const fontFamily = reader.fontFamily === 'serif' ? 'var(--font-serif)' : 'var(--font-sans)'
  const sourceHref = article.site_url || article.source_url
  const subscribeLabel = subscribed ? '已订阅' : subscribing ? '订阅中…' : '订阅此来源'
  const subscribeButton = (position: 'top' | 'bottom') => (
    <button
      type="button"
      onClick={() => void handleSubscribe()}
      disabled={subscribed || subscribing}
      aria-label={subscribeLabel}
      data-position={position}
    >
      {subscribeLabel}
    </button>
  )

  return (
    <div className="reading-layout explore-article-reader">
      <div className="reading-toolbar flex-between">
        <button type="button" className="reading-exit" aria-label="返回探索" onClick={goBack}>← 返回探索</button>
        <div className="explore-article-actions"><ExploreArticleActions actions={actions} />{subscribeButton('top')}</div>
      </div>

      {actions.error && <div className="explore-inline-error" role="alert">{actions.error}</div>}
      {subscribeError && <div className="explore-inline-error" role="alert">{subscribeError}</div>}

      <article
        className="reading-article"
        aria-label={article.title}
        style={{ fontSize: reader.fontSize, fontFamily }}
      >
        <h1 className="reading-title">{article.title}</h1>
        <div className="reading-meta">
          <a href={sourceHref} target="_blank" rel="noopener noreferrer">{article.source_title}</a>
          <span> · {formatDate(article.published_at)}</span>
          <span> · </span>
          <a href={article.url} target="_blank" rel="noopener noreferrer">原文链接</a>
        </div>

        {(contentLoading || article.content_status === 'fetching') && (
          <div role="status" className="explore-notice">正在获取原文，先显示订阅源提供的内容…</div>
        )}
        {!contentLoading && (contentError || article.content_status === 'failed') && (
          <div className="explore-inline-error" role="alert">
            <p>{contentError || article.content_error || '暂未获取到更多正文，已保留订阅源提供的内容。'}</p>
            {article.content_retry_at && <p className="text-muted text-sm">下次可重试：{new Date(article.content_retry_at).toLocaleString('zh-CN')}</p>}
            <button type="button" className="secondary" onClick={() => void fetchOriginal()}>重试获取原文</button>
          </div>
        )}

        {article.content ? (
          <CodeWrapContext.Provider value={reader.codeWrap}>
            <MarkdownArticle source={article.content} originalUrl={article.url} />
          </CodeWrapContext.Provider>
        ) : (
          <div className="text-muted">{article.excerpt || '暂无正文'}</div>
        )}

        <div className="reading-nav">
          <button type="button" className="reading-nav-btn" aria-label="返回探索列表" onClick={goBack}>返回探索</button>
          <ExploreArticleActions actions={actions} />{subscribeButton('bottom')}
        </div>
      </article>

      <ReaderSettingsPanel
        fontSize={reader.fontSize}
        fontFamily={reader.fontFamily}
        codeWrap={reader.codeWrap}
        onFontSize={reader.setFontSize}
        onFontFamily={reader.setFontFamily}
        onCodeWrap={reader.setCodeWrap}
      />
    </div>
  )
}
