import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import {
  getExploreArticle,
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
  const [subscribed, setSubscribed] = useState(Boolean(preview?.is_subscribed))
  const [subscribing, setSubscribing] = useState(false)
  const [subscribeError, setSubscribeError] = useState('')
  const loadGenerationRef = useRef(0)
  const subscribeGenerationRef = useRef(0)
  const subscribingRef = useRef(false)
  const clickedArticleIDsRef = useRef(new Set<number>())
  const completedArticleIDsRef = useRef(new Set<number>())

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
    setSubscribed(Boolean(preview?.is_subscribed))
    void loadArticle()
    return () => {
      loadGenerationRef.current += 1
      subscribeGenerationRef.current += 1
      subscribingRef.current = false
    }
  }, [loadArticle, preview?.is_subscribed])

  // ExplorePage records click before it navigates. A matching route preview is
  // the hand-off marker, so only direct/deep-link entries need a detail click.
  useEffect(() => {
    if (!article || preview || clickedArticleIDsRef.current.has(article.id)) return
    clickedArticleIDsRef.current.add(article.id)
    void recordExploreArticleEvent(article.id, 'click').catch(() => {})
  }, [article, preview])

  useEffect(() => {
    if (!article) return
    const articleIDAtMount = article.id
    const onScroll = () => {
      if (completedArticleIDsRef.current.has(articleIDAtMount)) return
      const progress = computeViewportProgress(
        window.scrollY,
        document.documentElement.scrollHeight,
        window.innerHeight,
      )
      if (progress < 0.9) return
      completedArticleIDsRef.current.add(articleIDAtMount)
      void recordExploreArticleEvent(articleIDAtMount, 'completed_read').catch(() => {})
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [article])

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
        {subscribeButton('top')}
      </div>

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

        {article.content ? (
          <CodeWrapContext.Provider value={reader.codeWrap}>
            <MarkdownArticle source={article.content} />
          </CodeWrapContext.Provider>
        ) : (
          <div className="text-muted">{article.excerpt || '暂无正文'}</div>
        )}

        <div className="reading-nav">
          <button type="button" className="reading-nav-btn" aria-label="返回探索列表" onClick={goBack}>返回探索</button>
          {subscribeButton('bottom')}
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
