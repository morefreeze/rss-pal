import { useState, useEffect, useCallback, useRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { getArticles, searchArticles, getRecommended, markAllRead, Article, Feed, getFeeds, likeArticle, dislikeArticle } from '../api/client'
import ReadingMeta from '../components/ReadingMeta'
import ArticleCard from '../components/ArticleCard'
import { usePlayer } from '../player/PlayerContext'
import { useExposureTracking, reportClick } from '../hooks/useExposureTracking'

const PAGE_SIZE = 20

// PREFETCH_OFFSET attaches the IntersectionObserver to the Nth-from-last
// article rather than a sentinel below the list, so the next page starts
// fetching while the user still has ~5 articles left to read.
const PREFETCH_OFFSET = 5

// MediaIndicator shows a per-article badge for media articles. Audio
// articles get a clickable ▶ play button (starts inline playback); video
// articles get a non-interactive 🎬 marker (video must play inside the
// article page where the embed lives). Rendered as siblings rather than
// either/or so an article that ever has both kinds of media displays
// both icons.
function MediaIndicator({ article, onPlay }: { article: Article; onPlay: (a: Article) => void }) {
  if (!article.media_url) return null
  const t = article.media_type ?? ''
  const isVideo = t.startsWith('video/')
  const isAudio = t.startsWith('audio/')
  // Articles with media_url but no recognised type fall back to the
  // play-button shape (the original behaviour).
  const audioFallback = !isVideo && !isAudio

  return (
    <span style={{ display: 'inline-flex', gap: 4, marginRight: 8, flexShrink: 0 }}>
      {isVideo && (
        <span
          title="视频"
          aria-label="视频"
          style={{
            padding: '2px 8px',
            borderRadius: 999,
            border: '1px solid #cc3a3a',
            background: '#fff5f5',
            color: '#cc3a3a',
            fontSize: 12,
          }}
        >
          🎬
        </span>
      )}
      {(isAudio || audioFallback) && (
        <button
          type="button"
          aria-label="播放"
          title="音频 · 点击播放"
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            onPlay(article)
          }}
          style={{
            padding: '2px 8px',
            borderRadius: 999,
            border: '1px solid #0066cc',
            background: '#fff',
            color: '#0066cc',
            fontSize: 12,
            cursor: 'pointer',
          }}
        >
          ▶
        </button>
      )}
    </span>
  )
}

// SearchArticleRow renders a single article card in the search-results panel.
function SearchArticleRow({
  article,
  isRead,
  isFocused,
  idx,
  onPlay,
  formatDate,
  stripMarkdown,
  onNavigate,
  onFocus,
  navList,
}: {
  article: Article
  isRead: boolean
  isFocused: boolean
  idx: number
  onPlay: (a: Article) => void
  formatDate: (d: string | null) => string
  stripMarkdown: (t: string) => string
  onNavigate: (id: number) => void
  onFocus: (idx: number) => void
  navList: number[]
}) {
  const exposureRef = useExposureTracking(article.id)

  return (
    <div
      ref={exposureRef}
      className="card"
      data-article-card
      style={{
        display: 'block',
        opacity: isRead ? 0.6 : 1,
        cursor: 'pointer',
        outline: isFocused ? '2px solid #0066cc' : 'none',
        outlineOffset: -2,
      }}
      onClick={() => {
        onFocus(idx)
        reportClick(article.id)
        try { sessionStorage.setItem('articleNavList', JSON.stringify(navList)) } catch {}
        onNavigate(article.id)
      }}
    >
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
        {!isRead && (
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#0066cc', flexShrink: 0, marginTop: 6 }} />
        )}
        <div style={{ flex: 1 }}>
          <div className={isRead ? 'text-muted' : 'text-bold'} style={{ display: 'flex', alignItems: 'center' }}>
            <MediaIndicator article={article} onPlay={onPlay} />
            <span>{article.title}</span>
          </div>
          {article.summary_brief && (
            <div className="text-muted text-sm mt-1">
              {stripMarkdown(article.summary_brief).slice(0, 120)}...
            </div>
          )}
          <div className="flex-between mt-1">
            <div className="flex gap-2" style={{ alignItems: 'center' }}>
              <span className="text-muted text-sm">{formatDate(article.published_at)}</span>
              <ReadingMeta wordCount={article.word_count} readingMinutes={article.reading_minutes} />
            </div>
            {article.feed_title && (
              <span className="text-sm" style={{ padding: '1px 6px', background: '#f0f4ff', borderRadius: 4, color: '#4b6bcc' }}>
                {article.feed_title}
              </span>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

export default function ArticleListPage() {
  const navigate = useNavigate()
  const player = usePlayer()
  const [articles, setArticles] = useState<Article[]>([])
  const [recommended, setRecommended] = useState<Article[]>([])
  const [boostedIds, setBoostedIds] = useState<Set<number>>(new Set())
  const [feeds, setFeeds] = useState<Feed[]>([])
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [selectedFeed, setSelectedFeed] = useState<number | null>(() => {
    try { return JSON.parse(sessionStorage.getItem('selectedFeed') || 'null') } catch { return null }
  })
  const [unreadOnly, setUnreadOnly] = useState(() => {
    try { return sessionStorage.getItem('unreadOnly') === 'true' } catch { return false }
  })
  const [savedOnly, setSavedOnly] = useState(() => {
    try { return sessionStorage.getItem('savedOnly') === 'true' } catch { return false }
  })
  const [showRecommended, setShowRecommended] = useState(() => {
    try { return localStorage.getItem('showRecommended') === 'true' } catch { return false }
  })
  const [offset, setOffset] = useState(0)
  const [hasMore, setHasMore] = useState(true)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<Article[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [markingAllRead, setMarkingAllRead] = useState(false)
  const loadMoreRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const [sessionReadIds, setSessionReadIds] = useState<Set<number>>(() => {
    try {
      return new Set(JSON.parse(sessionStorage.getItem('readArticles') || '[]'))
    } catch { return new Set() }
  })
  const [focusedIdx, setFocusedIdx] = useState<number>(-1)
  const searchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    loadFeeds()
    loadRecommended()
    const onRefreshUnread = () => {
      try {
        setSessionReadIds(new Set(JSON.parse(sessionStorage.getItem('readArticles') || '[]')))
      } catch {}
    }
    window.addEventListener('refresh-unread', onRefreshUnread)
    return () => window.removeEventListener('refresh-unread', onRefreshUnread)
  }, [])

  useEffect(() => {
    setOffset(0)
    setHasMore(true)
    setFocusedIdx(-1)
    loadArticles(0, true)
  }, [selectedFeed, unreadOnly, savedOnly])

  const loadFeeds = async () => {
    const data = await getFeeds()
    setFeeds(data || [])
  }

  const loadArticles = useCallback(async (off: number, reset: boolean) => {
    if (reset) setLoading(true)
    else setLoadingMore(true)

    try {
      const raw = await getArticles({
        feed_id: selectedFeed || undefined,
        unread: unreadOnly || undefined,
        saved: savedOnly || undefined,
        limit: PAGE_SIZE,
        offset: off,
      })
      const data = raw || []
      if (reset) {
        setArticles(data)
        // Restore scroll position when navigating back from article page
        try {
          const savedScroll = sessionStorage.getItem('articleListScroll')
          if (savedScroll) {
            sessionStorage.removeItem('articleListScroll')
            setTimeout(() => window.scrollTo(0, Number(savedScroll)), 50)
          }
        } catch {}
      } else {
        setArticles(prev => [...prev, ...data])
      }
      setHasMore(data.length === PAGE_SIZE)
      setOffset(off)
    } finally {
      setLoading(false)
      setLoadingMore(false)
    }
  }, [selectedFeed, unreadOnly, savedOnly])

  const loadMore = useCallback(() => {
    if (!loadingMore && hasMore) {
      loadArticles(offset + PAGE_SIZE, false)
    }
  }, [loadingMore, hasMore, offset, loadArticles])

  // Infinite scroll via IntersectionObserver
  useEffect(() => {
    if (!loadMoreRef.current) return
    const observer = new IntersectionObserver(
      entries => { if (entries[0].isIntersecting) loadMore() },
      { rootMargin: '200px' }
    )
    observer.observe(loadMoreRef.current)
    return () => observer.disconnect()
  }, [loadMore])

  const handleMarkAllRead = async () => {
    setMarkingAllRead(true)
    try {
      await markAllRead({ feedId: selectedFeed, unread: unreadOnly, saved: savedOnly })
      // Mirror the per-article session tracking so the list still reflects the
      // change before the next fetch, without clobbering reads from other filters.
      const markedIds = articles.map(a => a.id)
      try {
        const stored: number[] = JSON.parse(sessionStorage.getItem('readArticles') || '[]')
        const merged = Array.from(new Set([...stored, ...markedIds]))
        sessionStorage.setItem('readArticles', JSON.stringify(merged))
        setSessionReadIds(new Set(merged))
      } catch {}
      if (unreadOnly) {
        setArticles([])
        setHasMore(false)
      } else {
        setArticles(prev => prev.map(a => ({ ...a, is_read: true })))
      }
      window.dispatchEvent(new Event('refresh-unread'))
    } catch {
      // silent fail
    } finally {
      setMarkingAllRead(false)
    }
  }

  const loadRecommended = async () => {
    try {
      const data = await getRecommended(10)
      setRecommended(data || [])
    } catch {
      // Ignore errors for recommended
    }
  }

  const handleBoost = async (articleId: number, e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setBoostedIds(prev => {
      const next = new Set(prev)
      next.add(articleId)
      return next
    })
    try {
      await likeArticle(articleId)
    } catch {
      setBoostedIds(prev => {
        const next = new Set(prev)
        next.delete(articleId)
        return next
      })
    }
  }

  const handleDampen = async (articleId: number, e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setRecommended(prev => prev.filter(a => a.id !== articleId))
    try {
      await dislikeArticle(articleId)
      await loadRecommended()
    } catch {
      // Keep the row removed locally; next page load will resync.
    }
  }

  const handleSearchChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const q = e.target.value
    setSearchQuery(q)
    if (searchTimeout.current) clearTimeout(searchTimeout.current)
    if (!q.trim()) {
      setSearchResults(null)
      return
    }
    searchTimeout.current = setTimeout(async () => {
      setSearching(true)
      try {
        const results = await searchArticles(q.trim(), 30)
        setSearchResults(results || [])
      } catch {
        setSearchResults([])
      } finally {
        setSearching(false)
      }
    }, 400)
  }

  // Keyboard navigation: j/k moves focus, o/Enter opens focused article
  useEffect(() => {
    const displayedArticles = searchQuery
      ? (searchResults || [])
      : articles.filter(a => !unreadOnly || !sessionReadIds.has(a.id))

    const handler = (e: KeyboardEvent) => {
      if (['INPUT', 'TEXTAREA', 'SELECT'].includes((e.target as HTMLElement)?.tagName)) return
      if (e.key === '/') {
        e.preventDefault()
        searchRef.current?.focus()
        return
      }
      if (e.key === 'j' || e.key === 'ArrowDown') {
        e.preventDefault()
        setFocusedIdx(i => {
          const next = Math.min(i + 1, displayedArticles.length - 1)
          // scroll into view after state update
          setTimeout(() => {
            const cards = document.querySelectorAll('[data-article-card]')
            cards[next]?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
          }, 0)
          return next
        })
      } else if (e.key === 'k' || e.key === 'ArrowUp') {
        e.preventDefault()
        setFocusedIdx(i => {
          const prev = Math.max(i - 1, 0)
          setTimeout(() => {
            const cards = document.querySelectorAll('[data-article-card]')
            cards[prev]?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
          }, 0)
          return prev
        })
      } else if ((e.key === 'o' || e.key === 'Enter') && focusedIdx >= 0) {
        const article = displayedArticles[focusedIdx]
        if (article) openArticle(article.id)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [articles, searchResults, searchQuery, unreadOnly, sessionReadIds, focusedIdx])

  const isRead = (article: Article) => article.is_read || sessionReadIds.has(article.id)

  const openArticle = (id: number) => {
    reportClick(id)
    try {
      const ids = articles.map(a => a.id)
      const i = ids.indexOf(id)
      const start = Math.max(0, i - 50)
      const end = Math.min(ids.length, i + 51)
      sessionStorage.setItem('articleNavList', JSON.stringify(ids.slice(start, end)))
      sessionStorage.setItem('articleListScroll', String(window.scrollY))
      sessionStorage.setItem('articleEntryPath', '/articles')
    } catch {}
    navigate(`/articles/${id}`, { state: { from: '/articles' } })
  }

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return ''
    const date = new Date(dateStr)
    return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  const stripMarkdown = (text: string) =>
    text
      .replace(/[#*`_~>\[\]]/g, '')
      .replace(/\n+/g, ' ')
      .replace(/•\s*/g, '')
      .replace(/▸\s*/g, '')
      .replace(/\s{2,}/g, ' ')
      .trim()

  return (
    <div>
      <div className="flex-between mb-2">
        <h2>文章列表</h2>
        <div className="flex gap-2" style={{ flexWrap: 'wrap' }}>
          <input
            ref={searchRef}
            type="search"
            placeholder="搜索文章... ( / 聚焦)"
            value={searchQuery}
            onChange={handleSearchChange}
            style={{ padding: '6px 12px', width: 200 }}
          />
          <select
            value={selectedFeed || ''}
            onChange={e => {
              const val = e.target.value ? Number(e.target.value) : null
              setSelectedFeed(val)
              try { sessionStorage.setItem('selectedFeed', JSON.stringify(val)) } catch {}
            }}
            style={{ padding: '6px 12px' }}
            disabled={!!searchQuery}
          >
            <option value="">全部订阅</option>
            {feeds.map(f => (
              <option key={f.id} value={f.id}>{f.title || f.url}{f.unread_count > 0 ? ` (${f.unread_count})` : ''}</option>
            ))}
          </select>
          <label className="flex gap-1" style={{ alignItems: 'center' }}>
            <input
              type="checkbox"
              checked={unreadOnly}
              onChange={e => {
                setUnreadOnly(e.target.checked)
                if (e.target.checked) setSavedOnly(false)
                try { sessionStorage.setItem('unreadOnly', String(e.target.checked)) } catch {}
              }}
              disabled={!!searchQuery}
            />
            仅未读
          </label>
          <label className="flex gap-1" style={{ alignItems: 'center' }}>
            <input
              type="checkbox"
              checked={savedOnly}
              onChange={e => {
                setSavedOnly(e.target.checked)
                if (e.target.checked) setUnreadOnly(false)
                try { sessionStorage.setItem('savedOnly', String(e.target.checked)) } catch {}
              }}
              disabled={!!searchQuery}
            />
            收藏
          </label>
          {!searchQuery && (
            <button
              className="secondary"
              style={{ fontSize: 12, padding: '4px 10px' }}
              onClick={() => loadArticles(0, true)}
              disabled={loading}
              title="刷新文章列表"
            >
              ↻
            </button>
          )}
          {!searchQuery && articles.length > 0 && (
            <button
              className="secondary"
              style={{ fontSize: 12, padding: '4px 10px' }}
              onClick={handleMarkAllRead}
              disabled={markingAllRead}
            >
              {markingAllRead ? '处理中...' : '全部已读'}
            </button>
          )}
        </div>
      </div>

      {recommended.length > 0 && !searchQuery && (
        <div className="rec-panel">
          <button
            type="button"
            className="rec-panel-header"
            aria-expanded={showRecommended}
            title={showRecommended ? '收起' : '展开'}
            onClick={() => {
              const next = !showRecommended
              setShowRecommended(next)
              try { localStorage.setItem('showRecommended', String(next)) } catch {}
            }}
          >
            <span aria-hidden className="rec-panel-arrow">
              {showRecommended ? '▼' : '▶'}
            </span>
            <h3 className="rec-panel-title">为你推荐</h3>
            <span className="text-muted text-sm" style={{ fontWeight: 'normal' }}>({recommended.length})</span>
          </button>
          {showRecommended && recommended.map(article => (
            <Link key={article.id} to={`/articles/${article.id}`} className="rec-row">
              <div className="flex-between">
                <div style={{ flex: 1 }}>
                  <div className="text-bold" style={{ display: 'flex', alignItems: 'center' }}>
                    <MediaIndicator article={article} onPlay={player.playArticle} />
                    <span>{article.title}</span>
                  </div>
                  <div className="flex gap-2 mt-1">
                    <span className="text-muted text-sm">{formatDate(article.published_at)}</span>
                    {article.feed_title && (
                      <span className="text-sm" style={{ padding: '1px 6px', background: '#f0f4ff', borderRadius: 4, color: '#4b6bcc' }}>
                        {article.feed_title}
                      </span>
                    )}
                  </div>
                </div>
                <div className="rec-feedback">
                  {boostedIds.has(article.id) ? (
                    <span className="rec-feedback-badge">✓ 已多推</span>
                  ) : (
                    <>
                      <button
                        type="button"
                        className="rec-feedback-btn"
                        title="多推这类"
                        aria-label="多推这类"
                        onClick={(e) => handleBoost(article.id, e)}
                      >👍</button>
                      <button
                        type="button"
                        className="rec-feedback-btn"
                        title="少推这类"
                        aria-label="少推这类"
                        onClick={(e) => handleDampen(article.id, e)}
                      >👎</button>
                    </>
                  )}
                </div>
              </div>
            </Link>
          ))}
        </div>
      )}

      {/* Search results */}
      {searchQuery && (
        searching ? (
          <div className="card text-muted text-sm">搜索中...</div>
        ) : searchResults !== null ? (
          searchResults.length === 0 ? (
            <div className="card text-muted">未找到相关文章</div>
          ) : (
            <>
              <div className="text-muted text-sm mb-1" style={{ padding: '0 4px' }}>找到 {searchResults.length} 篇文章</div>
              {searchResults.map((article, idx) => (
                <SearchArticleRow
                  key={article.id}
                  article={article}
                  isRead={isRead(article)}
                  isFocused={focusedIdx === idx}
                  idx={idx}
                  onPlay={player.playArticle}
                  formatDate={formatDate}
                  stripMarkdown={stripMarkdown}
                  onNavigate={(id) => navigate(`/articles/${id}`)}
                  onFocus={setFocusedIdx}
                  navList={searchResults.map(a => a.id)}
                />
              ))}
            </>
          )
        ) : null
      )}

      {!searchQuery && loading ? (
        <div className="card">Loading...</div>
      ) : !searchQuery && articles.length === 0 && feeds.length === 0 ? (
        <div className="card" style={{ textAlign: 'center', padding: '32px 16px' }}>
          <div style={{ fontSize: 40, marginBottom: 12 }}>📰</div>
          <div className="text-bold" style={{ marginBottom: 8, fontSize: 16 }}>还没有订阅</div>
          <div className="text-muted" style={{ marginBottom: 16 }}>去「订阅」页面添加你感兴趣的 RSS 源或网站，系统会自动抓取并生成 AI 摘要</div>
          <Link to="/feeds"><button>去添加订阅</button></Link>
        </div>
      ) : !searchQuery && articles.length === 0 ? (
        <div className="card text-muted">
          {unreadOnly ? '没有未读文章 🎉' : '暂无文章，订阅源正在抓取中...'}
        </div>
      ) : !searchQuery ? (() => {
        const filtered = articles.filter(a => !unreadOnly || !sessionReadIds.has(a.id))
        // Prefetch trigger sits PREFETCH_OFFSET items before the end so
        // the next page starts loading while the user still has reading
        // left. Falls back to position 0 when the list is shorter than
        // the offset, ensuring the observer attaches to *some* card and
        // the bottom-of-list visual indicator is purely informational.
        const prefetchIdx = hasMore && filtered.length > 0
          ? Math.max(0, filtered.length - PREFETCH_OFFSET)
          : -1
        return (
        <>
          {filtered.map((article, idx) => (
            <ArticleCard
              key={article.id}
              article={article}
              manualTags={[]}
              isRead={isRead(article)}
              isFocused={focusedIdx === idx}
              idx={idx}
              prefetchRef={idx === prefetchIdx ? loadMoreRef : undefined}
              onPlay={player.playArticle}
              formatDate={formatDate}
              stripMarkdown={stripMarkdown}
              onOpen={openArticle}
              onFocus={setFocusedIdx}
            />
          ))}
          {hasMore ? (
            <div style={{ textAlign: 'center', padding: '12px', color: '#999', fontSize: 13 }}>
              {loadingMore ? '加载中...' : ''}
            </div>
          ) : articles.length > 0 ? (
            <div style={{ textAlign: 'center', padding: '16px', color: '#ccc', fontSize: 13 }}>
              — 已加载全部文章 —
            </div>
          ) : null}
        </>
        )
      })() : null}
    </div>
  )
}
