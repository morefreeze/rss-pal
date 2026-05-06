import { useState, useEffect, useCallback, useRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { getArticles, searchArticles, getRecommended, markAllRead, Article, Feed, getFeeds } from '../api/client'
import ReadingMeta from '../components/ReadingMeta'

const PAGE_SIZE = 20

export default function ArticleListPage() {
  const navigate = useNavigate()
  const [articles, setArticles] = useState<Article[]>([])
  const [recommended, setRecommended] = useState<Article[]>([])
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
  const [showRecommended, setShowRecommended] = useState(true)
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
      await markAllRead()
      // Clear session read tracking — everything is now read
      try { sessionStorage.removeItem('readArticles') } catch {}
      setSessionReadIds(new Set())
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
    try {
      sessionStorage.setItem('articleNavList', JSON.stringify(articles.map(a => a.id)))
      sessionStorage.setItem('articleListScroll', String(window.scrollY))
    } catch {}
    navigate(`/articles/${id}`)
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

      {showRecommended && recommended.length > 0 && !searchQuery && (
        <div className="mb-2">
          <div className="flex-between mb-1">
            <h3>为你推荐</h3>
            <button className="secondary text-sm" onClick={() => setShowRecommended(false)}>收起</button>
          </div>
          {recommended.map(article => (
            <Link key={article.id} to={`/articles/${article.id}`} className="card" style={{ display: 'block' }}>
              <div className="flex-between">
                <div style={{ flex: 1 }}>
                  <div className="text-bold">{article.title}</div>
                  <div className="flex gap-2 mt-1">
                    <span className="text-muted text-sm">{formatDate(article.published_at)}</span>
                    {article.feed_title && (
                      <span className="text-sm" style={{ padding: '1px 6px', background: '#f0f4ff', borderRadius: 4, color: '#4b6bcc' }}>
                        {article.feed_title}
                      </span>
                    )}
                  </div>
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
                <div
                  key={article.id}
                  className="card"
                  data-article-card
                  style={{
                    display: 'block',
                    opacity: isRead(article) ? 0.6 : 1,
                    cursor: 'pointer',
                    outline: focusedIdx === idx ? '2px solid #0066cc' : 'none',
                    outlineOffset: -2,
                  }}
                  onClick={() => {
                    setFocusedIdx(idx)
                    try { sessionStorage.setItem('articleNavList', JSON.stringify(searchResults.map(a => a.id))) } catch {}
                    navigate(`/articles/${article.id}`)
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
                    {!isRead(article) && (
                      <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#0066cc', flexShrink: 0, marginTop: 6 }} />
                    )}
                    <div style={{ flex: 1 }}>
                      <div className={isRead(article) ? 'text-muted' : 'text-bold'}>{article.title}</div>
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
      ) : !searchQuery ? (
        <>
          {articles.filter(a => !unreadOnly || !sessionReadIds.has(a.id)).map((article, idx) => (
            <div
              key={article.id}
              className="card"
              data-article-card
              style={{
                display: 'block',
                opacity: isRead(article) ? 0.6 : 1,
                cursor: 'pointer',
                outline: focusedIdx === idx ? '2px solid #0066cc' : 'none',
                outlineOffset: -2,
              }}
              onClick={() => { setFocusedIdx(idx); openArticle(article.id) }}
            >
              <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
                {!isRead(article) && (
                  <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#0066cc', flexShrink: 0, marginTop: 6 }} />
                )}
                <div style={{ flex: 1 }}>
                  <div className={isRead(article) ? 'text-muted' : 'text-bold'}>{article.title}</div>
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
          ))}
          {hasMore ? (
            <div ref={loadMoreRef} style={{ textAlign: 'center', padding: '12px', color: '#999', fontSize: 13 }}>
              {loadingMore ? '加载中...' : ''}
            </div>
          ) : articles.length > 0 ? (
            <div style={{ textAlign: 'center', padding: '16px', color: '#ccc', fontSize: 13 }}>
              — 已加载全部文章 —
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  )
}
