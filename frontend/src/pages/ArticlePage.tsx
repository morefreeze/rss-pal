import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useNavigate, useLocation } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import {
  getArticle, fetchContent, likeArticle, dislikeArticle, saveArticle, unsaveArticle,
  recordReadDuration, updateProgress, resetProgress,
  getTemplates, generateSummaryStream, shareArticle, exportMarkdown, expandLinkSetChild,
  confirmLinkSetSuggestion, hideArticle, unhideArticle,
  Article, ReadingProgress, SummaryTemplate
} from '../api/client'
import { toast } from '../utils/toast'
import { LinkSetChildren } from '../components/LinkSetChildren'
import { BatchFetchModal } from '../components/BatchFetchModal'
import ReadingMeta from '../components/ReadingMeta'
import MarkdownArticle from '../components/MarkdownArticle'
import ReadingLayout from '../components/ReadingLayout'
import BackToTopButton from '../components/BackToTopButton'
import BackFab from '../components/BackFab'
import { ArticleDetailSkeleton } from '../components/ArticleDetailSkeleton'
import ConfettiBurst from '../components/ConfettiBurst'
import { useReaderSettings } from '../hooks/useReaderSettings'
import { useReadingChrome } from '../hooks/useReadingChrome'
import ArticlePlayerCard from '../components/ArticlePlayerCard'
import TagBar from '../components/TagBar'
import CollapsibleFab from '../components/CollapsibleFab'
import { CodeWrapContext } from '../components/CodeWrapContext'
import ArticleActionsMenu from '../components/ArticleActionsMenu'
import { readNavList, readNavContext, writeNav, fetchMoreIds } from '../utils/articleNav'

export default function ArticlePage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const location = useLocation()
  const reader = useReaderSettings()
  const { toggle: toggleReadingChrome } = useReadingChrome(reader.mode === 'reading')
  const entryPath =
    (location.state as { from?: string } | null)?.from
    ?? (() => { try { return sessionStorage.getItem('articleEntryPath') } catch { return null } })()
    ?? '/articles'
  const handleBack = useCallback(() => navigate(entryPath), [navigate, entryPath])
  const [article, setArticle] = useState<Article | null>(null)
  const [progress, setProgress] = useState<ReadingProgress | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')

  // Prev/next nav. The list page snapshots visible article IDs into
  // sessionStorage; optionally it also stores a fetch context so we can
  // extend the snapshot on demand when the user hits Next at the end of a
  // paginated list that still has more articles server-side.
  const [navList, setNavList] = useState<number[]>(readNavList)
  const [navContext, setNavContext] = useState(readNavContext)
  const [loadingNext, setLoadingNext] = useState(false)
  // Single in-flight extend fetch — shared by both the silent prefetch effect
  // and a click on Next, so a click during prefetch awaits the same request
  // instead of starting a second.
  const pendingExtendRef = useRef<Promise<number | null> | null>(null)
  const currentIdx = id ? navList.indexOf(Number(id)) : -1
  const prevId = currentIdx > 0 ? navList[currentIdx - 1] : null
  const directNextId = currentIdx >= 0 && currentIdx < navList.length - 1 ? navList[currentIdx + 1] : null
  const atEndOfList = currentIdx >= 0 && currentIdx === navList.length - 1
  // Next is available either directly from the snapshot or via on-demand
  // fetch when we're at the end of the snapshot but more pages exist.
  const hasNext = !!directNextId || (atEndOfList && !!navContext)

  const goToArticle = useCallback((nextArticleId: number) => {
    navigate(`/articles/${nextArticleId}`, { replace: true, state: { from: entryPath } })
  }, [navigate, entryPath])

  // Fetch the next page, extend navList + persist, return the first new id.
  // Returns null when the server has nothing more (also drops the context so
  // future renders compute hasNext=false).
  const extendNavList = useCallback(async (): Promise<number | null> => {
    if (!navContext) return null
    const { ids, stillMore } = await fetchMoreIds(navContext)
    if (ids.length === 0) {
      writeNav(navList, null)
      setNavContext(null)
      return null
    }
    const extended = [...navList, ...ids]
    const nextCtx = stillMore
      ? { ...navContext, nextOffset: navContext.nextOffset + navContext.pageSize }
      : null
    writeNav(extended, nextCtx)
    setNavList(extended)
    setNavContext(nextCtx)
    return ids[0]
  }, [navContext, navList])

  // Background prefetch: when the user lands on the last article of the
  // snapshot and a fetch context exists, silently load the next page so
  // clicking Next is instant. Mirrors the list page's scroll prefetch.
  useEffect(() => {
    if (!atEndOfList || !navContext || pendingExtendRef.current) return
    const p = extendNavList().catch(() => null)
    pendingExtendRef.current = p
    p.finally(() => {
      if (pendingExtendRef.current === p) pendingExtendRef.current = null
    })
  }, [atEndOfList, navContext, extendNavList])

  const goNext = useCallback(async () => {
    if (directNextId) {
      goToArticle(directNextId)
      return
    }
    if (!atEndOfList) return
    setLoadingNext(true)
    try {
      // Reuse the in-flight prefetch if any; otherwise kick a fresh fetch.
      const firstNewId = await (pendingExtendRef.current ?? extendNavList())
      if (firstNewId != null) goToArticle(firstNewId)
    } catch {
      // Network failure: leave state alone so the user can retry.
    } finally {
      setLoadingNext(false)
    }
  }, [directNextId, atEndOfList, extendNavList, goToArticle])

  const goPrev = useCallback(() => {
    if (prevId) goToArticle(prevId)
  }, [prevId, goToArticle])
  const [fetchingContent, setFetchingContent] = useState(false)
  const [liked, setLiked] = useState(false)
  const [disliked, setDisliked] = useState(false)
  const [saved, setSaved] = useState(false)
  const contentRef = useRef<HTMLDivElement>(null)
  const summaryRef = useRef<HTMLDivElement>(null)
  const readStartTime = useRef<number>(Date.now())
  // Tracks which article id has already had its scroll position restored, so
  // later progress updates don't yank the user back to the saved position.
  const scrollRestoredForRef = useRef<number | null>(null)
  // High-water mark so scrolling up (e.g. to nav buttons) can't regress saved progress.
  const maxScrollRef = useRef<number>(0)
  // Progress (0..1) at which the AI summary card has just exited the viewport.
  // null while we can't measure (no summary content, layout not ready).
  const [aiMarkerPos, setAiMarkerPos] = useState<number | null>(null)
  const [confettiFired, setConfettiFired] = useState(false)
  const [showCelebration, setShowCelebration] = useState(false)
  const celebrationTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Template selector state
  const [templates, setTemplates] = useState<SummaryTemplate[]>([])
  const [selectedTemplateId, setSelectedTemplateId] = useState<number | undefined>(undefined)

  // Streaming summary state
  const [streamingBrief, setStreamingBrief] = useState('')
  const [streamingDetailed, setStreamingDetailed] = useState('')
  const [streamPhase, setStreamPhase] = useState<'idle' | 'brief' | 'detailed'>('idle')
  const streamAbortRef = useRef<AbortController | null>(null)

  // Share state
  const [shareToken, setShareToken] = useState<string>('')
  const [copyLinkText, setCopyLinkText] = useState('复制链接')

  // Bookmarklet state
  const [fromBookmarklet, setFromBookmarklet] = useState(false)

  // Soft-delete state: true when the article is hidden for this user. Surface
  // is a banner that offers 恢复 — the article itself still renders normally.
  const [hidden, setHidden] = useState(false)

  // LinkSet children
  const [linkSetChildren, setLinkSetChildren] = useState<Article[] | null>(null)

  // Batch fetch modal
  const [batchModalOpen, setBatchModalOpen] = useState(false)

  const loadArticle = async () => {
    if (!id) return
    setLoading(true)
    setLoadError('')
    try {
      const data = await getArticle(Number(id))
      setArticle(data.article)
      setProgress(data.progress)
      // Clamp: historical rows may have >1 from before write-side clamping,
      // and without this they'd block all future scroll persistence (handleScroll
      // requires scrollPosition > maxScrollRef.current to write).
      maxScrollRef.current = Math.min(1, data.progress?.scroll_position ?? 0)
      setFromBookmarklet(Boolean(data.from_bookmarklet))
      setHidden(Boolean(data.hidden))
      setLinkSetChildren(data.children ?? null)
      if (data.signals) {
        setLiked((data.signals['like'] ?? 0) > 0)
        setDisliked((data.signals['dislike'] ?? 0) > 0)
        setSaved((data.signals['save'] ?? 0) > 0)
      }
    } catch (err: any) {
      if (err?.response?.status === 404) {
        setLoadError('文章不存在或无权访问')
      } else {
        setLoadError('加载失败，请稍后重试')
      }
    } finally {
      setLoading(false)
    }
    readStartTime.current = Date.now()
  }

  useEffect(() => {
    scrollRestoredForRef.current = null
    loadArticle()

    return () => {
      // Record read duration on unmount
      const duration = (Date.now() - readStartTime.current) / 1000
      if (id && duration > 5) {
        recordReadDuration(Number(id), duration)
      }
      streamAbortRef.current?.abort()
    }
  }, [id])

  // Restore scroll to the saved reading position, once per article load.
  // Waits for images currently in the DOM to finish loading so scrollHeight
  // is stable before scrolling; caps the wait at 5s. Skips restoration when
  // saved progress is > 0.9 — at that point the user has effectively finished
  // the article, so reopening it should start from the top.
  useEffect(() => {
    if (!article) return
    if (scrollRestoredForRef.current === article.id) return
    const saved = progress?.scroll_position ?? 0
    scrollRestoredForRef.current = article.id
    if (saved <= 0 || saved > 0.9) return

    let done = false

    const performScroll = () => {
      if (done) return
      done = true
      if (window.scrollY > 50) return
      const max = (contentRef.current?.scrollHeight ?? 0) - window.innerHeight
      if (max > 0) window.scrollTo(0, max * saved)
    }

    const root = contentRef.current
    const pending = root
      ? Array.from(root.querySelectorAll('img')).filter(img => !img.complete)
      : []

    if (pending.length === 0) {
      requestAnimationFrame(performScroll)
      return
    }

    let remaining = pending.length
    const onOne = () => {
      remaining -= 1
      if (remaining <= 0) performScroll()
    }
    pending.forEach(img => {
      img.addEventListener('load', onOne, { once: true })
      img.addEventListener('error', onOne, { once: true })
    })

    const timer = setTimeout(performScroll, 5000)

    return () => {
      done = true
      clearTimeout(timer)
      pending.forEach(img => {
        img.removeEventListener('load', onOne)
        img.removeEventListener('error', onOne)
      })
    }
  }, [article?.id, progress?.scroll_position])

  // Auto-expand stub link_set children and poll until ready/failed
  useEffect(() => {
    if (!article) return
    const state = article.processing_state
    if (state !== 'stub' && state !== 'processing') return

    let cancelled = false
    let intervalId: ReturnType<typeof setInterval> | null = null

    const startPolling = () => {
      intervalId = setInterval(async () => {
        if (cancelled) return
        try {
          const data = await getArticle(article.id)
          if (cancelled) return
          if (data.article.processing_state === 'ready' || data.article.processing_state === 'failed') {
            setArticle(data.article)
            setLinkSetChildren(data.children ?? null)
            if (intervalId) clearInterval(intervalId)
          } else {
            setArticle(data.article)
          }
        } catch (e) {
          console.warn('article poll failed', e)
        }
      }, 4000)
    }

    if (state === 'stub') {
      expandLinkSetChild(article.id)
        .then(() => { if (!cancelled) startPolling() })
        .catch((err) => console.warn('expand failed', err))
    } else {
      // state === 'processing' — already queued, just poll
      startPolling()
    }

    // 5-minute safety cap
    const safetyTimer = setTimeout(() => {
      cancelled = true
      if (intervalId) clearInterval(intervalId)
    }, 5 * 60 * 1000)

    return () => {
      cancelled = true
      if (intervalId) clearInterval(intervalId)
      clearTimeout(safetyTimer)
    }
  }, [article?.id, article?.processing_state])

  // Keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Skip if focused in an input/textarea
      if (['INPUT', 'TEXTAREA', 'SELECT'].includes((e.target as HTMLElement)?.tagName)) return
      if (e.key === 'n' || e.key === 'j') {
        if (hasNext) goNext()
      } else if (e.key === 'p' || e.key === 'k') {
        goPrev()
      } else if (e.key === 'Escape' || e.key === 'Backspace') {
        handleBack()
      } else if (e.key === 'm') {
        if (article) {
          if (progress?.is_completed) handleMarkUnread()
          else handleMarkRead()
        }
      } else if (e.key === 'r') {
        reader.toggleMode()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [hasNext, goNext, goPrev, article, progress, reader, handleBack])

  // Load templates on mount
  useEffect(() => {
    getTemplates().then(ts => setTemplates(ts || [])).catch(() => {})
  }, [])

  // Accumulates active (visible) seconds on this page for the stay-time gate.
  const activeReadSecondsRef = useRef(0)

  const pendingProgressRef = useRef<{ scrollPosition: number; isCompleted: boolean } | null>(null)
  const progressTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const flushProgress = useCallback(async () => {
    if (!article) return
    const pending = pendingProgressRef.current
    if (!pending) return
    pendingProgressRef.current = null
    if (progressTimerRef.current) {
      clearTimeout(progressTimerRef.current)
      progressTimerRef.current = null
    }
    try {
      const newProgress = await updateProgress(article.id, pending.scrollPosition, pending.isCompleted)
      setProgress(newProgress)
    } catch {
      // network blip — let the next scroll re-schedule
    }
  }, [article])

  const scheduleProgressFlush = useCallback(() => {
    if (progressTimerRef.current) clearTimeout(progressTimerRef.current)
    progressTimerRef.current = setTimeout(() => {
      flushProgress()
    }, 1500)
  }, [flushProgress])

  // Counts active seconds and acts as the completion backstop. handleScroll
  // only re-evaluates is_completed on scroll events, so if the user reaches
  // the bottom before the time gate elapses, no later scroll event fires and
  // the article would stay forever-unread. This tick fills that hole.
  useEffect(() => {
    const tick = setInterval(() => {
      if (document.visibilityState !== 'visible') return
      activeReadSecondsRef.current += 1

      if (!article || progress?.is_completed) return
      if (maxScrollRef.current <= 0.9) return
      const readMin = article.reading_minutes || 1
      const minSeconds = Math.min(15, Math.floor(readMin * 30))
      if (activeReadSecondsRef.current < minSeconds) return

      const scrollPosition = Math.max(maxScrollRef.current, 0.9)
      setProgress(prev => prev
        ? { ...prev, scroll_position: scrollPosition, is_completed: true }
        : { id: 0, article_id: article.id, scroll_position: scrollPosition, last_read_at: new Date().toISOString(), is_completed: true })
      pendingProgressRef.current = { scrollPosition, isCompleted: true }
      try {
        const read = JSON.parse(sessionStorage.getItem('readArticles') || '[]')
        if (!read.includes(article.id)) {
          read.push(article.id)
          sessionStorage.setItem('readArticles', JSON.stringify(read))
        }
      } catch {}
      window.dispatchEvent(new Event('refresh-unread'))
      flushProgress()
    }, 1000)
    return () => clearInterval(tick)
  }, [article, progress?.is_completed, flushProgress])

  const handleScroll = useCallback(() => {
    if (!article || !contentRef.current) return

    const scrollTop = window.scrollY
    const scrollHeight = contentRef.current.scrollHeight - window.innerHeight
    // Clamp to [0,1]: iOS rubber-band overscroll and mid-load scrollHeight
    // shrinkage can otherwise push the ratio past 1, which then gets persisted
    // and displays as >100% on reload.
    const rawPosition = scrollHeight > 0 ? scrollTop / scrollHeight : 0
    const scrollPosition = Math.min(1, Math.max(0, rawPosition))

    // Monotonic: only persist when we've read further than before.
    if (scrollPosition <= maxScrollRef.current) return
    maxScrollRef.current = scrollPosition

    const readMin = article?.reading_minutes || 1
    const minSeconds = Math.min(15, Math.floor(readMin * 30))
    const isCompleted = scrollPosition > 0.9 && activeReadSecondsRef.current >= minSeconds
    const wasCompleted = progress?.is_completed

    setProgress(prev => prev
      ? { ...prev, scroll_position: scrollPosition, is_completed: isCompleted }
      : { id: 0, article_id: article.id, scroll_position: scrollPosition, last_read_at: new Date().toISOString(), is_completed: isCompleted })

    pendingProgressRef.current = { scrollPosition, isCompleted }

    if (isCompleted && !wasCompleted) {
      try {
        const read = JSON.parse(sessionStorage.getItem('readArticles') || '[]')
        if (!read.includes(article.id)) {
          read.push(article.id)
          sessionStorage.setItem('readArticles', JSON.stringify(read))
        }
      } catch {}
      window.dispatchEvent(new Event('refresh-unread'))
      flushProgress()
      return
    }

    scheduleProgressFlush()
  }, [article, progress, flushProgress, scheduleProgressFlush])

  useEffect(() => {
    window.addEventListener('scroll', handleScroll)
    return () => window.removeEventListener('scroll', handleScroll)
  }, [handleScroll])

  // Reset confetti state when navigating between articles.
  useEffect(() => {
    setConfettiFired(false)
    setShowCelebration(false)
    if (celebrationTimerRef.current) {
      clearTimeout(celebrationTimerRef.current)
      celebrationTimerRef.current = null
    }
  }, [id])

  // Measure where the AI summary card ends, expressed as a 0..1 fraction of
  // total scrollable height. Re-measure after the summary or content changes,
  // and on a short delay to catch late-loading images/markdown.
  useEffect(() => {
    const hasSummary = !!(article?.summary_brief || article?.summary_detailed)
    if (!hasSummary || streamPhase !== 'idle') {
      setAiMarkerPos(null)
      return
    }
    const recompute = () => {
      const summary = summaryRef.current
      const content = contentRef.current
      if (!summary || !content) { setAiMarkerPos(null); return }
      const maxScroll = content.scrollHeight - window.innerHeight
      if (maxScroll <= 0) { setAiMarkerPos(null); return }
      const summaryBottom = summary.offsetTop + summary.offsetHeight
      const scrollAtPast = Math.max(0, summaryBottom - window.innerHeight)
      const pos = scrollAtPast / maxScroll
      setAiMarkerPos(pos > 0.01 && pos < 0.99 ? pos : null)
    }
    recompute()
    const t1 = setTimeout(recompute, 300)
    const t2 = setTimeout(recompute, 1200)
    window.addEventListener('resize', recompute)
    return () => {
      clearTimeout(t1)
      clearTimeout(t2)
      window.removeEventListener('resize', recompute)
    }
  }, [article?.id, article?.summary_brief, article?.summary_detailed, article?.content, streamPhase])

  // Fire confetti the first time the user scrolls past the AI marker. If the
  // page is already past the marker on mount (scroll-restored), mark as fired
  // silently — no retroactive celebration.
  useEffect(() => {
    if (aiMarkerPos === null || confettiFired) return
    const content = contentRef.current
    if (!content) return
    const maxScroll = content.scrollHeight - window.innerHeight
    if (maxScroll <= 0) return
    if (window.scrollY / maxScroll >= aiMarkerPos) {
      setConfettiFired(true)
      return
    }
    const onScroll = () => {
      const m = contentRef.current
      if (!m) return
      const ms = m.scrollHeight - window.innerHeight
      if (ms <= 0) return
      if (window.scrollY / ms >= aiMarkerPos) {
        setConfettiFired(true)
        if (reader.confettiEnabled) {
          setShowCelebration(true)
          if (celebrationTimerRef.current) clearTimeout(celebrationTimerRef.current)
          celebrationTimerRef.current = setTimeout(() => setShowCelebration(false), 1900)
        }
      }
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [aiMarkerPos, confettiFired, reader.confettiEnabled])

  useEffect(() => {
    const onVisibility = () => { if (document.hidden) flushProgress() }
    const onBeforeUnload = () => { flushProgress() }
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => {
      document.removeEventListener('visibilitychange', onVisibility)
      window.removeEventListener('beforeunload', onBeforeUnload)
      flushProgress()
    }
  }, [flushProgress])

  const handleRegenerateWithTemplate = async () => {
    if (!article) return
    streamAbortRef.current?.abort()
    const ctrl = new AbortController()
    streamAbortRef.current = ctrl

    setStreamingBrief('')
    setStreamingDetailed('')
    setStreamPhase('brief')

    let finalBrief = ''
    let finalDetailed = ''

    await generateSummaryStream(
      article.id,
      selectedTemplateId,
      {
        onBriefDelta: (t) => setStreamingBrief(prev => prev + t),
        onBriefPhaseDone: () => setStreamPhase('detailed'),
        onBriefDone: (full) => {
          finalBrief = full
          setStreamingBrief(full)
        },
        onDetailedDelta: (t) => {
          setStreamPhase(prev => prev === 'brief' ? 'detailed' : prev)
          setStreamingDetailed(prev => prev + t)
        },
        onDetailedDone: (full) => {
          finalDetailed = full
          setStreamingDetailed(full)
        },
        onDone: () => {
          setArticle(a => a ? { ...a, summary_brief: finalBrief, summary_detailed: finalDetailed } : a)
          setStreamPhase('idle')
          setStreamingBrief('')
          setStreamingDetailed('')
        },
        onError: (msg) => {
          toast.error('生成总结失败：' + msg)
          setStreamPhase('idle')
          setStreamingBrief('')
          setStreamingDetailed('')
        },
      },
      ctrl.signal,
    )
  }

  const handleFetchContent = async () => {
    if (!article) return
    setFetchingContent(true)
    try {
      const result = await fetchContent(article.id)
      setArticle({ ...article, content: result.content })
    } catch {
      toast.error('获取内容失败')
    } finally {
      setFetchingContent(false)
    }
  }

  const handleRescrapeViaBookmarklet = () => {
    if (!article) return
    const ok = window.confirm(
      `重新抓取需要在原网页运行书签。\n` +
      `会打开 ${article.url}，请到新标签页点你 bookmark bar 上的 RSS Pal 书签来抓取最新内容。\n\n` +
      `继续？`
    )
    if (!ok) return
    window.open(article.url, '_blank', 'noopener,noreferrer')
    toast.info('已打开原网页 — 在新标签里点你的 RSS Pal 书签')
  }

  const handleLike = async () => {
    if (!article) return
    if (liked) {
      setLiked(false)
    } else {
      await likeArticle(article.id)
      setLiked(true)
      setDisliked(false)
    }
  }

  const handleDislike = async () => {
    if (!article) return
    if (disliked) {
      setDisliked(false)
    } else {
      await dislikeArticle(article.id)
      setDisliked(true)
      setLiked(false)
    }
  }

  const handleSave = async () => {
    if (!article) return
    if (saved) {
      await unsaveArticle(article.id)
      setSaved(false)
    } else {
      await saveArticle(article.id)
      setSaved(true)
    }
  }

  const handleDelete = async () => {
    if (!article) return
    const targetId = article.id
    // Capture nav before the hide, in case the navList changes shape later.
    const goAfter = hasNext ? goNext : handleBack
    try {
      await hideArticle(targetId)
    } catch {
      toast.error('删除失败，请稍后重试')
      return
    }
    // Local hint so list pages can hide the row immediately on remount.
    try {
      const arr: number[] = JSON.parse(sessionStorage.getItem('hiddenArticles') || '[]')
      if (!arr.includes(targetId)) {
        arr.push(targetId)
        sessionStorage.setItem('hiddenArticles', JSON.stringify(arr))
      }
    } catch {}
    window.dispatchEvent(new Event('refresh-unread'))
    toast.info('已删除', {
      action: {
        label: '撤销',
        onClick: async () => {
          try {
            await unhideArticle(targetId)
            try {
              const arr: number[] = JSON.parse(sessionStorage.getItem('hiddenArticles') || '[]')
              sessionStorage.setItem('hiddenArticles', JSON.stringify(arr.filter(i => i !== targetId)))
            } catch {}
            window.dispatchEvent(new Event('refresh-unread'))
            navigate(`/articles/${targetId}`, { replace: true, state: { from: entryPath } })
          } catch {
            toast.error('撤销失败')
          }
        },
      },
    })
    goAfter()
  }

  const handleRestore = async () => {
    if (!article) return
    try {
      await unhideArticle(article.id)
      setHidden(false)
      try {
        const arr: number[] = JSON.parse(sessionStorage.getItem('hiddenArticles') || '[]')
        sessionStorage.setItem('hiddenArticles', JSON.stringify(arr.filter(i => i !== article.id)))
      } catch {}
      window.dispatchEvent(new Event('refresh-unread'))
      toast.success('已恢复')
    } catch {
      toast.error('恢复失败')
    }
  }

  const handleMarkRead = async () => {
    if (!article) return
    const newProgress = await updateProgress(article.id, 1.0, true)
    setProgress(newProgress)
    maxScrollRef.current = 1.0
    try {
      const read = JSON.parse(sessionStorage.getItem('readArticles') || '[]')
      if (!read.includes(article.id)) {
        read.push(article.id)
        sessionStorage.setItem('readArticles', JSON.stringify(read))
      }
    } catch {}
    window.dispatchEvent(new Event('refresh-unread'))
  }

  const handleMarkUnread = async () => {
    if (!article) return
    await resetProgress(article.id)
    setProgress(null)
    maxScrollRef.current = 0
    try {
      const read: number[] = JSON.parse(sessionStorage.getItem('readArticles') || '[]')
      sessionStorage.setItem('readArticles', JSON.stringify(read.filter(id => id !== article.id)))
    } catch {}
    window.dispatchEvent(new Event('refresh-unread'))
  }

  const getOrFetchShareToken = async (): Promise<string> => {
    if (shareToken) return shareToken
    if (!article) return ''
    const result = await shareArticle(article.id)
    setShareToken(result.token)
    return result.token
  }

  const handleShareTwitter = async () => {
    if (!article) return
    try {
      const token = await getOrFetchShareToken()
      const shareUrl = window.location.origin + '/share/' + token
      window.open(
        'https://twitter.com/intent/tweet?text=' + encodeURIComponent(article.title) + '&url=' + encodeURIComponent(shareUrl),
        '_blank'
      )
    } catch {
      toast.error('获取分享链接失败')
    }
  }

  const handleShareXiaohongshu = () => {
    if (!article) return
    const feedTitle = (article as any).feed_title || ''
    const text = `📖 ${article.title}\n\n${article.summary_brief || ''}\n\n🔗 ${article.url}\n#RSS阅读 #${feedTitle}`
    navigator.clipboard.writeText(text).then(() => {
      toast.success('已复制到剪贴板，去小红书粘贴发布吧！')
    }).catch(() => {
      toast.error('复制失败，请手动复制')
    })
  }

  const handleExportMarkdown = async () => {
    if (!article) return
    let mdContent: string
    try {
      mdContent = await exportMarkdown(article.id)
    } catch {
      // Fallback: generate markdown locally
      const contentPreview = article.content ? article.content.slice(0, 2000) : ''
      mdContent = `# ${article.title}\n\n> 来源：${article.url}\n\n## 摘要\n\n${article.summary_brief || ''}\n\n---\n\n${contentPreview}`
    }
    const blob = new Blob([mdContent], { type: 'text/markdown;charset=utf-8' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = article.title.slice(0, 30) + '.md'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(a.href)
  }

  const handleCopyLink = async () => {
    if (!article) return
    try {
      const token = await getOrFetchShareToken()
      const shareUrl = window.location.origin + '/share/' + token
      await navigator.clipboard.writeText(shareUrl)
      setCopyLinkText('已复制！')
      setTimeout(() => setCopyLinkText('复制链接'), 2000)
    } catch {
      toast.error('复制失败，请手动复制')
    }
  }

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return ''
    return new Date(dateStr).toLocaleString('zh-CN')
  }

  const progressPercent = progress?.scroll_position ? Math.min(100, Math.round(progress.scroll_position * 100)) : 0

  if (loading && !article) return (
    <div className="card">
      <ArticleDetailSkeleton />
    </div>
  )
  if (loading) return <div className="card">Loading...</div>
  if (loadError || !article) return (
    <div className="card" style={{ textAlign: 'center' }}>
      <div className="text-muted">{loadError || '文章不存在'}</div>
      <button className="secondary" style={{ marginTop: 12 }} onClick={handleBack}>← 返回</button>
    </div>
  )

  if (reader.mode === 'reading') {
    return (
      <ReadingLayout
        article={{
          title: article.title,
          url: article.url,
          published_at: article.published_at,
          word_count: article.word_count ?? 0,
          reading_minutes: article.reading_minutes ?? 0,
          content: article.content,
          summary_brief: article.summary_brief,
          summary_detailed: article.summary_detailed,
        }}
        fontSize={reader.fontSize}
        fontFamily={reader.fontFamily}
        codeWrap={reader.codeWrap}
        onExit={() => reader.setMode('normal')}
        onFontSize={reader.setFontSize}
        onFontFamily={reader.setFontFamily}
        onCodeWrap={reader.setCodeWrap}
        onTapBody={toggleReadingChrome}
        onPrev={prevId ? goPrev : undefined}
        onNext={hasNext && !loadingNext ? goNext : undefined}
        onBack={handleBack}
      />
    )
  }

  return (
    <div ref={contentRef}>
      {/* Sticky progress bar at top of viewport — always visible so the
          AI marker shows up from the moment the article opens. */}
      <div style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        height: 4,
        backgroundColor: '#e0e0e0',
        zIndex: 1000,
      }}>
        <div style={{
          height: '100%',
          width: `${progressPercent}%`,
          backgroundColor: '#0066cc',
          transition: 'width 0.3s ease',
        }} />
        {aiMarkerPos !== null && (
          <div
            className={`ai-marker${showCelebration ? ' pulse' : ''}`}
            style={{ left: `${aiMarkerPos * 100}%` }}
            title="AI 总结结束"
            aria-label="AI summary end"
          >
            🏁
            {showCelebration && reader.confettiEnabled && <ConfettiBurst />}
          </div>
        )}
      </div>

      {hidden && (
        <div
          className="p-3 rounded-md mb-4 text-sm flex items-center gap-3"
          style={{ border: '1px solid #fca5a5', background: '#fef2f2', color: '#991b1b' }}
        >
          <span>🗑 这篇文章已删除 · 仍可查看</span>
          <button
            type="button"
            className="secondary"
            onClick={handleRestore}
            style={{ fontSize: 13, padding: '2px 10px', marginLeft: 'auto' }}
          >
            恢复
          </button>
        </div>
      )}

      <div className="card">
        <div className="flex-between mb-2">
          <div className="flex gap-1">
            <button
              className="secondary"
              onClick={handleBack}
              style={{ fontSize: 13, padding: '4px 10px' }}
            >
              ← 返回
            </button>
            <button
              className="secondary"
              disabled={!prevId}
              onClick={goPrev}
              style={{ fontSize: 13, padding: '4px 10px' }}
              title="上一篇"
            >
              ‹ 上一篇
            </button>
            <button
              className="secondary"
              disabled={!hasNext || loadingNext}
              onClick={goNext}
              style={{ fontSize: 13, padding: '4px 10px' }}
              title="下一篇"
            >
              {loadingNext ? '加载中…' : '下一篇 ›'}
            </button>
          </div>
          {article.feed_title && (
            <div className="text-sm" style={{ color: 'var(--accent)' }}>{article.feed_title}</div>
          )}
        </div>
        <h2>{article.title}</h2>
        <div className="text-muted text-sm mb-2" style={{ display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center' }}>
          <span>{formatDate(article.published_at)}</span>
          <ReadingMeta wordCount={article.word_count} readingMinutes={article.reading_minutes} />
          <span>·</span>
          <a href={article.url} target="_blank" rel="noopener noreferrer">原文链接</a>
          {progressPercent > 0 && <span>· 阅读进度 {progressPercent}%</span>}
        </div>

        <TagBar articleId={article.id} />

        <ArticlePlayerCard article={article} />

        <div className="flex gap-2 mb-2" style={{ flexWrap: 'wrap' }}>
          <button
            onClick={handleLike}
            style={liked ? { backgroundColor: '#22c55e', color: 'white' } : {}}
          >
            {liked ? '✓ 已喜欢' : '👍 喜欢'}
          </button>
          <button
            className="secondary"
            onClick={handleDislike}
            style={disliked ? { backgroundColor: '#ef4444', color: 'white', borderColor: '#ef4444' } : {}}
          >
            {disliked ? '✓ 已不喜欢' : '👎 不喜欢'}
          </button>
          <button
            className="secondary"
            onClick={handleSave}
            style={saved ? { backgroundColor: '#f59e0b', color: 'white', borderColor: '#f59e0b' } : {}}
          >
            {saved ? '✓ 已保存' : '⭐ 保存'}
          </button>
          {progress?.is_completed ? (
            <button
              className="secondary"
              onClick={handleMarkUnread}
              style={{ fontSize: 13 }}
            >
              ↩ 标记未读
            </button>
          ) : (
            <button
              className="secondary"
              onClick={handleMarkRead}
              style={{ fontSize: 13 }}
            >
              ✓ 标记已读
            </button>
          )}
          <button
            className="secondary"
            onClick={() => reader.setMode('reading')}
            title="进入阅读模式 (r)"
            style={{ fontSize: 13 }}
          >
            📖 阅读模式
          </button>
          <ArticleActionsMenu onDelete={handleDelete} />
        </div>
      </div>

      {/* Summary-stage banner — content-stage fetch is shown inline in the
          原文内容 card below so the prompt sits where the body would be.
          For PDF clip articles (state=processing && no content yet), the
          OCR banner below is more informative and replaces this one. */}
      {(() => {
        const state = article.processing_state
        const hasContent = !!article.content && article.content.length > 0
        if (state === 'processing' && hasContent) {
          return (
            <div className="p-3 rounded-md mb-4 text-sm" style={{ border: '1px solid var(--border)', background: 'var(--bg-elevated)', color: 'var(--fg-muted)' }}>
              正在生成摘要…
            </div>
          )
        }
        return null
      })()}
      {article.processing_state === 'processing' && (
        <div
          style={{
            padding: '12px',
            background: '#fff3cd',
            border: '1px solid #ffeaa7',
            borderRadius: '4px',
            marginBottom: '16px',
          }}
        >
          ⏳ 这篇 PDF 正在 OCR 处理中（约 1–5 秒/页，多页 PDF 可能需要几分钟）。处理完成后内容会自动出现，刷新页面即可。
        </div>
      )}
      {article.processing_state === 'failed' && article.processing_error && (
        <div
          style={{
            padding: '12px',
            background: '#f8d7da',
            border: '1px solid #f5c6cb',
            borderRadius: '4px',
            marginBottom: '16px',
          }}
        >
          ❌ PDF 处理失败：{article.processing_error}
        </div>
      )}
      {article.processing_state === 'failed' && !article.processing_error && (
        <div className="p-3 rounded-md mb-4 text-sm flex items-center gap-3" style={{ border: '1px solid var(--border)', background: 'var(--bg-elevated)' }}>
          <span style={{ color: 'var(--fg-muted)' }}>抓取失败</span>
          <button
            type="button"
            className="px-2 py-1 text-xs rounded"
            style={{ border: '1px solid var(--border)', color: 'var(--fg)' }}
            onClick={async () => {
              try {
                await expandLinkSetChild(article.id)
                const data = await getArticle(article.id)
                setArticle(data.article)
                setLinkSetChildren(data.children ?? null)
              } catch (e) {
                console.warn('retry failed', e)
              }
            }}
          >
            重试
          </button>
        </div>
      )}

      {/* Summary section — shown before content */}
      <div ref={summaryRef} className="card">
        <div className="flex-between mb-2">
          <h3>AI 总结</h3>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
            <select
              value={selectedTemplateId ?? ''}
              onChange={e => setSelectedTemplateId(e.target.value ? Number(e.target.value) : undefined)}
              style={{ fontSize: 13, padding: '2px 6px' }}
            >
              <option value="">默认模板</option>
              {templates.map(t => (
                <option key={t.id} value={t.id}>{t.name}{t.is_system ? '' : ' ★'}</option>
              ))}
            </select>
            <button
              className={article.summary_brief || article.summary_detailed ? 'secondary' : ''}
              onClick={handleRegenerateWithTemplate}
              disabled={streamPhase !== 'idle'}
              style={{ fontSize: 13, padding: '4px 12px' }}
            >
              {streamPhase !== 'idle' ? '生成中...' : (article.summary_brief || article.summary_detailed) ? '重新生成' : '生成总结'}
            </button>
          </div>
        </div>

        {streamPhase !== 'idle' ? (
          <div className="markdown-body">
            {streamingBrief && (
              <div style={{ whiteSpace: 'pre-wrap' }}>
                {streamingBrief}
                {streamPhase === 'brief' && <span className="typing-caret">▍</span>}
              </div>
            )}
            {streamingDetailed && (
              <>
                <hr style={{ margin: '12px 0', borderColor: 'var(--border)' }} />
                <div style={{ whiteSpace: 'pre-wrap' }}>
                  {streamingDetailed}
                  {streamPhase === 'detailed' && <span className="typing-caret">▍</span>}
                </div>
              </>
            )}
            {!streamingBrief && !streamingDetailed && (
              <div className="text-muted text-sm" style={{ padding: '8px 0' }}>正在生成总结...</div>
            )}
          </div>
        ) : (article.summary_brief || article.summary_detailed) ? (
          <div className="markdown-body">
            {article.summary_brief && (
              <ReactMarkdown>{article.summary_brief}</ReactMarkdown>
            )}
            {article.summary_detailed && (
              <>
                <hr style={{ margin: '12px 0', borderColor: 'var(--border)' }} />
                <ReactMarkdown>{article.summary_detailed}</ReactMarkdown>
              </>
            )}
          </div>
        ) : (
          <div className="text-muted text-sm" style={{ padding: '8px 0' }}>
            暂无总结，点击右上角"生成总结"按钮
          </div>
        )}
      </div>

      {/* Content section: hidden entirely for video articles without
           transcript yet (the worker handles them; original-page scraping
           produces useless boilerplate for YouTube/Bilibili watch pages). */}
      {(() => {
        const isVideo = article.media_type?.startsWith('video/')
        if (isVideo && !article.content) return null
        const state = article.processing_state
        const hasContent = !!article.content && article.content.length > 0
        const workerFetching = state === 'stub' || (state === 'processing' && !hasContent)
        const refetchDisabled = fetchingContent || workerFetching
        const refetchTitle = workerFetching ? '正在抓取' : undefined
        return (
          <div className="card">
            <div className="flex-between mb-1">
              <h3>{isVideo ? '字幕' : '原文内容'}</h3>
              {!isVideo && (fromBookmarklet ? (
                <button onClick={handleRescrapeViaBookmarklet} title="在新标签打开原网页，由你点击书签来更新">
                  🔁 通过书签重新抓取
                </button>
              ) : (
                <button onClick={handleFetchContent} disabled={refetchDisabled} title={refetchTitle}>
                  {fetchingContent ? '获取中...' : '重新抓取'}
                </button>
              ))}
            </div>
            {article.content ? (
              <div style={{ lineHeight: 1.8, fontSize: 15 }}>
                <CodeWrapContext.Provider value={reader.codeWrap}>
                  <MarkdownArticle source={article.content} />
                </CodeWrapContext.Provider>
              </div>
            ) : workerFetching ? (
              <div className="text-muted">正在抓取...</div>
            ) : (
              <div className="text-muted">暂无内容，点击"重新抓取"从原文链接抓取</div>
            )}
          </div>
        )
      })()}

      {/* Share section */}
      <div className="card">
        <div className="flex-between">
          <span style={{ fontWeight: 600, fontSize: 15 }}>分享到：</span>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <button
              className="secondary"
              onClick={handleShareTwitter}
              title="分享到 X (Twitter)"
            >
              𝕏
            </button>
            <button
              className="secondary"
              onClick={handleShareXiaohongshu}
              title="分享到小红书"
            >
              小红书
            </button>
            <button
              className="secondary"
              onClick={handleExportMarkdown}
              title="导出 Markdown"
            >
              MD
            </button>
            <button
              className="secondary"
              onClick={handleCopyLink}
              title="复制分享链接"
            >
              {copyLinkText}
            </button>
          </div>
        </div>
      </div>
      {article.links_extendable === true && (
        <CollapsibleFab
          articleId={article.id}
          icon="📥"
          label="批量抓取"
          variant="primary"
          title="检测到多个可抓取链接"
          onActivate={() => setBatchModalOpen(true)}
        />
      )}
      {article.links_extendable !== true && article.link_set_suggested === true && (
        <CollapsibleFab
          articleId={article.id}
          icon="💡"
          label="转为 link_set"
          variant="outline"
          title="文章看起来是一组链接列表，确认后可批量抓取"
          onActivate={async () => {
            try {
              await confirmLinkSetSuggestion(article.id)
              const data = await getArticle(article.id)
              setArticle(data.article)
              setLinkSetChildren(data.children ?? null)
              setBatchModalOpen(true)
            } catch (e) {
              console.warn('confirm link_set failed', e)
              toast.error('转换失败，请稍后重试')
            }
          }}
        />
      )}
      <BatchFetchModal
        open={batchModalOpen}
        articleId={article.id}
        onClose={() => setBatchModalOpen(false)}
        onFetched={async (_n) => {
          // Refresh the article to pick up new children
          try {
            const data = await getArticle(article.id)
            setArticle(data.article)
            setLinkSetChildren(data.children ?? null)
          } catch (e) {
            console.warn('refresh after batch_fetch failed', e)
          }
        }}
      />
      {article?.is_link_set && (
        <LinkSetChildren
          parentId={article.id}
          children={linkSetChildren ?? []}
          onChildrenUpdated={(updated) => setLinkSetChildren(updated)}
        />
      )}
      {/* Bottom nav so readers don't have to scroll back up to switch articles.
          BackFab handles the back-to-list affordance. */}
      <div className="card" style={{ display: 'flex', gap: 8, flexWrap: 'wrap', justifyContent: 'space-between' }}>
        <button
          className="secondary"
          disabled={!prevId}
          onClick={goPrev}
          style={{ fontSize: 13, padding: '6px 14px' }}
        >
          ‹ 上一篇
        </button>
        <button
          className="secondary"
          disabled={!hasNext || loadingNext}
          onClick={goNext}
          style={{ fontSize: 13, padding: '6px 14px' }}
        >
          {loadingNext ? '加载中…' : '下一篇 ›'}
        </button>
      </div>
      <BackFab onClick={handleBack} />
      <BackToTopButton />
    </div>
  )
}
