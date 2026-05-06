import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import {
  getArticle, fetchContent, likeArticle, dislikeArticle, saveArticle, unsaveArticle,
  recordReadDuration, updateProgress, resetProgress,
  getTemplates, generateSummaryStream, shareArticle, exportMarkdown,
  Article, ReadingProgress, SummaryTemplate
} from '../api/client'
import { toast } from '../utils/toast'
import ReadingMeta from '../components/ReadingMeta'
import MarkdownArticle from '../components/MarkdownArticle'
import ReadingLayout from '../components/ReadingLayout'
import BackToTopButton from '../components/BackToTopButton'
import { useReaderSettings } from '../hooks/useReaderSettings'

export default function ArticlePage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const reader = useReaderSettings()
  const [article, setArticle] = useState<Article | null>(null)
  const [progress, setProgress] = useState<ReadingProgress | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')

  // Compute prev/next from session nav list
  const navList: number[] = (() => {
    try { return JSON.parse(sessionStorage.getItem('articleNavList') || '[]') } catch { return [] }
  })()
  const currentIdx = id ? navList.indexOf(Number(id)) : -1
  const prevId = currentIdx > 0 ? navList[currentIdx - 1] : null
  const nextId = currentIdx >= 0 && currentIdx < navList.length - 1 ? navList[currentIdx + 1] : null
  const [fetchingContent, setFetchingContent] = useState(false)
  const [liked, setLiked] = useState(false)
  const [disliked, setDisliked] = useState(false)
  const [saved, setSaved] = useState(false)
  const contentRef = useRef<HTMLDivElement>(null)
  const readStartTime = useRef<number>(Date.now())
  const topTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

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

  const loadArticle = async () => {
    if (!id) return
    setLoading(true)
    setLoadError('')
    try {
      const data = await getArticle(Number(id))
      setArticle(data.article)
      setProgress(data.progress)
      setFromBookmarklet(Boolean(data.from_bookmarklet))
      if (data.signals) {
        setLiked((data.signals['like'] ?? 0) > 0)
        setDisliked((data.signals['dislike'] ?? 0) > 0)
        setSaved((data.signals['save'] ?? 0) > 0)
      }
      // Track as viewed in session so list can show it as read on back-navigation
      try {
        const read = JSON.parse(sessionStorage.getItem('readArticles') || '[]')
        if (!read.includes(Number(id))) {
          read.push(Number(id))
          sessionStorage.setItem('readArticles', JSON.stringify(read))
        }
      } catch {}

      // Scroll to saved position
      const savedProgress = data.progress?.scroll_position
      if (savedProgress && contentRef.current) {
        setTimeout(() => {
          const scrollHeight = contentRef.current?.scrollHeight || 0
          window.scrollTo(0, scrollHeight * savedProgress)
        }, 100)
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
    loadArticle()

    return () => {
      // Record read duration on unmount
      const duration = (Date.now() - readStartTime.current) / 1000
      if (id && duration > 5) {
        recordReadDuration(Number(id), duration)
      }
      if (topTimer.current) {
        clearTimeout(topTimer.current)
      }
      streamAbortRef.current?.abort()
    }
  }, [id])

  // Keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Skip if focused in an input/textarea
      if (['INPUT', 'TEXTAREA', 'SELECT'].includes((e.target as HTMLElement)?.tagName)) return
      if (e.key === 'n' || e.key === 'j') {
        if (nextId) navigate(`/articles/${nextId}`)
      } else if (e.key === 'p' || e.key === 'k') {
        if (prevId) navigate(`/articles/${prevId}`)
      } else if (e.key === 'Escape' || e.key === 'Backspace') {
        navigate(-1)
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
  }, [nextId, prevId, article, progress, navigate, reader])

  // Load templates on mount
  useEffect(() => {
    getTemplates().then(ts => setTemplates(ts || [])).catch(() => {})
  }, [])

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

  const handleScroll = useCallback(() => {
    if (!article || !contentRef.current) return

    const scrollTop = window.scrollY
    const scrollHeight = contentRef.current.scrollHeight - window.innerHeight
    const scrollPosition = scrollHeight > 0 ? scrollTop / scrollHeight : 0

    if (scrollTop === 0) {
      if (!topTimer.current) {
        topTimer.current = setTimeout(async () => {
          if (id) {
            await resetProgress(Number(id))
            setProgress(prev => prev ? { ...prev, scroll_position: 0, is_completed: false } : null)
          }
        }, 10000)
      }
      return
    }

    if (topTimer.current) {
      clearTimeout(topTimer.current)
      topTimer.current = null
    }

    const isCompleted = scrollPosition > 0.9
    const wasCompleted = progress?.is_completed

    // Update local UI immediately so the progress bar stays smooth
    setProgress(prev => prev ? {
      ...prev,
      scroll_position: scrollPosition,
      is_completed: isCompleted,
    } : prev)

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
  }, [article, id, progress, flushProgress, scheduleProgressFlush])

  useEffect(() => {
    window.addEventListener('scroll', handleScroll)
    return () => window.removeEventListener('scroll', handleScroll)
  }, [handleScroll])

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

  const handleMarkRead = async () => {
    if (!article) return
    const newProgress = await updateProgress(article.id, 1.0, true)
    setProgress(newProgress)
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

  const progressPercent = progress?.scroll_position ? Math.round(progress.scroll_position * 100) : 0

  if (loading) return <div className="card">Loading...</div>
  if (loadError || !article) return (
    <div className="card" style={{ textAlign: 'center' }}>
      <div className="text-muted">{loadError || '文章不存在'}</div>
      <button className="secondary" style={{ marginTop: 12 }} onClick={() => navigate(-1)}>← 返回</button>
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
        bgTheme={reader.bgTheme}
        onExit={() => reader.setMode('normal')}
        onFontSize={reader.setFontSize}
        onFontFamily={reader.setFontFamily}
        onBgTheme={reader.setBgTheme}
      />
    )
  }

  return (
    <div ref={contentRef}>
      {/* Sticky progress bar at top of viewport */}
      {progressPercent > 0 && (
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
        </div>
      )}

      <div className="card">
        <div className="flex-between mb-2">
          <div className="flex gap-1">
            <button
              className="secondary"
              onClick={() => navigate(-1)}
              style={{ fontSize: 13, padding: '4px 10px' }}
            >
              ← 返回
            </button>
            {prevId && (
              <button
                className="secondary"
                onClick={() => navigate(`/articles/${prevId}`)}
                style={{ fontSize: 13, padding: '4px 10px' }}
                title="上一篇"
              >
                ‹ 上一篇
              </button>
            )}
            {nextId && (
              <button
                className="secondary"
                onClick={() => navigate(`/articles/${nextId}`)}
                style={{ fontSize: 13, padding: '4px 10px' }}
                title="下一篇"
              >
                下一篇 ›
              </button>
            )}
          </div>
          {article.feed_title && (
            <div className="text-sm" style={{ color: '#4b6bcc' }}>{article.feed_title}</div>
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
        </div>
      </div>

      {/* Summary section — shown before content */}
      <div className="card">
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
                <hr style={{ margin: '12px 0', borderColor: '#eee' }} />
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
                <hr style={{ margin: '12px 0', borderColor: '#eee' }} />
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

      {/* Content section */}
      <div className="card">
        <div className="flex-between mb-1">
          <h3>原文内容</h3>
          {fromBookmarklet ? (
            <button onClick={handleRescrapeViaBookmarklet} title="在新标签打开原网页，由你点击书签来更新">
              🔁 通过书签重新抓取
            </button>
          ) : (
            <button onClick={handleFetchContent} disabled={fetchingContent}>
              {fetchingContent ? '获取中...' : '重新抓取'}
            </button>
          )}
        </div>
        {article.content ? (
          <div style={{ lineHeight: 1.8, fontSize: 15 }}>
            <MarkdownArticle source={article.content} />
          </div>
        ) : (
          <div className="text-muted">暂无内容，点击"重新抓取"从原文链接抓取</div>
        )}
      </div>

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
      <BackToTopButton />
    </div>
  )
}
