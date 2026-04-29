import { useState, useEffect, useRef, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import {
  getArticle, fetchContent, likeArticle, dislikeArticle, saveArticle,
  recordReadDuration, updateProgress, resetProgress,
  getTemplates, generateSummaryWithTemplate, shareArticle, exportMarkdown,
  Article, ReadingProgress, SummaryTemplate
} from '../api/client'

export default function ArticlePage() {
  const { id } = useParams<{ id: string }>()
  const [article, setArticle] = useState<Article | null>(null)
  const [progress, setProgress] = useState<ReadingProgress | null>(null)
  const [loading, setLoading] = useState(true)
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
  const [regenerating, setRegenerating] = useState(false)

  // Share state
  const [shareToken, setShareToken] = useState<string>('')
  const [copyLinkText, setCopyLinkText] = useState('复制链接')

  const loadArticle = async () => {
    if (!id) return
    setLoading(true)
    try {
      const data = await getArticle(Number(id))
      setArticle(data.article)
      setProgress(data.progress)

      // Scroll to saved position
      const savedProgress = data.progress?.scroll_position
      if (savedProgress && contentRef.current) {
        setTimeout(() => {
          const scrollHeight = contentRef.current?.scrollHeight || 0
          window.scrollTo(0, scrollHeight * savedProgress)
        }, 100)
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
    }
  }, [id])

  // Load templates on mount
  useEffect(() => {
    getTemplates().then(ts => setTemplates(ts || [])).catch(() => {})
  }, [])

  const handleScroll = useCallback(async () => {
    if (!article || !contentRef.current) return

    const scrollTop = window.scrollY
    const scrollHeight = contentRef.current.scrollHeight - window.innerHeight
    const scrollPosition = scrollHeight > 0 ? scrollTop / scrollHeight : 0

    // Detect if scrolled to top for 10+ seconds (reset progress)
    if (scrollTop === 0) {
      if (!topTimer.current) {
        topTimer.current = setTimeout(async () => {
          if (id) {
            await resetProgress(Number(id))
            setProgress(prev => prev ? { ...prev, scroll_position: 0, is_completed: false } : null)
          }
        }, 10000)
      }
    } else {
      if (topTimer.current) {
        clearTimeout(topTimer.current)
        topTimer.current = null
      }

      // Update progress
      const isCompleted = scrollPosition > 0.9
      const newProgress = await updateProgress(article.id, scrollPosition, isCompleted)
      setProgress(newProgress)
    }
  }, [article, id])

  useEffect(() => {
    window.addEventListener('scroll', handleScroll)
    return () => window.removeEventListener('scroll', handleScroll)
  }, [handleScroll])

  const handleRegenerateWithTemplate = async () => {
    if (!article) return
    setRegenerating(true)
    try {
      const result = await generateSummaryWithTemplate(article.id, selectedTemplateId)
      setArticle({ ...article, summary_brief: result.summary_brief, summary_detailed: result.summary_detailed })
    } catch {
      alert('重新生成总结失败')
    } finally {
      setRegenerating(false)
    }
  }

  const handleFetchContent = async () => {
    if (!article) return
    setFetchingContent(true)
    try {
      const result = await fetchContent(article.id)
      setArticle({ ...article, content: result.content })
    } catch {
      alert('获取内容失败')
    } finally {
      setFetchingContent(false)
    }
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
      setSaved(false)
    } else {
      await saveArticle(article.id)
      setSaved(true)
    }
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
      alert('获取分享链接失败')
    }
  }

  const handleShareXiaohongshu = () => {
    if (!article) return
    const feedTitle = (article as any).feed_title || ''
    const text = `📖 ${article.title}\n\n${article.summary_brief || ''}\n\n🔗 ${article.url}\n#RSS阅读 #${feedTitle}`
    navigator.clipboard.writeText(text).then(() => {
      alert('已复制到剪贴板，去小红书粘贴发布吧！')
    }).catch(() => {
      alert('复制失败，请手动复制')
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
      alert('复制失败，请手动复制')
    }
  }

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return ''
    return new Date(dateStr).toLocaleString('zh-CN')
  }

  const progressPercent = progress?.scroll_position ? Math.round(progress.scroll_position * 100) : 0

  if (loading) return <div className="card">Loading...</div>
  if (!article) return <div className="card">文章不存在</div>

  const readingTime = article.content
    ? Math.max(1, Math.round(article.content.replace(/\s+/g, ' ').split(' ').length / 200))
    : 0

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
        {article.feed_title && (
          <div className="text-sm mb-1" style={{ color: '#4b6bcc' }}>{article.feed_title}</div>
        )}
        <h2>{article.title}</h2>
        <div className="text-muted text-sm mb-2">
          {formatDate(article.published_at)}
          {readingTime > 0 && <span> · 约 {readingTime} 分钟</span>}
          <span> · </span>
          <a href={article.url} target="_blank" rel="noopener noreferrer">原文链接</a>
          {progressPercent > 0 && <span> · 阅读进度 {progressPercent}%</span>}
        </div>

        <div className="flex gap-2 mb-2">
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
              disabled={regenerating}
              style={{ fontSize: 13, padding: '4px 12px' }}
            >
              {(regenerating) ? '生成中...' : (article.summary_brief || article.summary_detailed) ? '重新生成' : '生成总结'}
            </button>
          </div>
        </div>

        {(article.summary_brief || article.summary_detailed) ? (
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
            {(regenerating) ? '正在生成总结...' : '暂无总结，点击右上角"生成总结"按钮'}
          </div>
        )}
      </div>

      {/* Content section */}
      <div className="card">
        <div className="flex-between mb-1">
          <h3>原文内容</h3>
          <button onClick={handleFetchContent} disabled={fetchingContent}>
            {fetchingContent ? '获取中...' : '重新抓取'}
          </button>
        </div>
        {article.content ? (
          <div style={{ whiteSpace: 'pre-wrap', lineHeight: 1.8, fontSize: 15 }}>{article.content}</div>
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
    </div>
  )
}
