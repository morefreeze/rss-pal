import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { getArticles, getRecommended, Article, Feed, getFeeds } from '../api/client'

const PAGE_SIZE = 20

export default function ArticleListPage() {
  const [articles, setArticles] = useState<Article[]>([])
  const [recommended, setRecommended] = useState<Article[]>([])
  const [feeds, setFeeds] = useState<Feed[]>([])
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [selectedFeed, setSelectedFeed] = useState<number | null>(null)
  const [unreadOnly, setUnreadOnly] = useState(false)
  const [showRecommended, setShowRecommended] = useState(true)
  const [offset, setOffset] = useState(0)
  const [hasMore, setHasMore] = useState(true)

  useEffect(() => {
    loadFeeds()
    loadRecommended()
  }, [])

  useEffect(() => {
    setOffset(0)
    setHasMore(true)
    loadArticles(0, true)
  }, [selectedFeed, unreadOnly])

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
        limit: PAGE_SIZE,
        offset: off,
      })
      const data = raw || []
      if (reset) {
        setArticles(data)
      } else {
        setArticles(prev => [...prev, ...data])
      }
      setHasMore(data.length === PAGE_SIZE)
      setOffset(off)
    } finally {
      setLoading(false)
      setLoadingMore(false)
    }
  }, [selectedFeed, unreadOnly])

  const loadMore = () => {
    loadArticles(offset + PAGE_SIZE, false)
  }

  const loadRecommended = async () => {
    try {
      const data = await getRecommended(10)
      setRecommended(data || [])
    } catch {
      // Ignore errors for recommended
    }
  }

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return ''
    const date = new Date(dateStr)
    return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  return (
    <div>
      <div className="flex-between mb-2">
        <h2>文章列表</h2>
        <div className="flex gap-2">
          <select
            value={selectedFeed || ''}
            onChange={e => setSelectedFeed(e.target.value ? Number(e.target.value) : null)}
            style={{ padding: '6px 12px' }}
          >
            <option value="">全部订阅</option>
            {feeds.map(f => (
              <option key={f.id} value={f.id}>{f.title || f.url}</option>
            ))}
          </select>
          <label className="flex gap-1" style={{ alignItems: 'center' }}>
            <input
              type="checkbox"
              checked={unreadOnly}
              onChange={e => setUnreadOnly(e.target.checked)}
            />
            仅未读
          </label>
        </div>
      </div>

      {showRecommended && recommended.length > 0 && (
        <div className="mb-2">
          <div className="flex-between mb-1">
            <h3>推荐</h3>
            <button className="secondary text-sm" onClick={() => setShowRecommended(false)}>收起</button>
          </div>
          {recommended.map(article => (
            <Link key={article.id} to={`/articles/${article.id}`} className="card" style={{ display: 'block' }}>
              <div className="text-bold">{article.title}</div>
              <div className="text-muted text-sm">{formatDate(article.published_at)}</div>
            </Link>
          ))}
        </div>
      )}

      {loading ? (
        <div className="card">Loading...</div>
      ) : articles.length === 0 ? (
        <div className="card text-muted">暂无文章</div>
      ) : (
        <>
          {articles.map(article => (
            <Link key={article.id} to={`/articles/${article.id}`} className="card" style={{ display: 'block' }}>
              <div className="text-bold">{article.title}</div>
              {article.summary_brief && (
                <div className="text-muted text-sm mt-1" style={{ whiteSpace: 'pre-line' }}>
                  {article.summary_brief.slice(0, 150)}...
                </div>
              )}
              <div className="flex-between mt-1">
                <span className="text-muted text-sm">{formatDate(article.published_at)}</span>
                {article.feed_title && (
                  <span className="text-sm" style={{ padding: '1px 6px', background: '#f0f4ff', borderRadius: 4, color: '#4b6bcc' }}>
                    {article.feed_title}
                  </span>
                )}
              </div>
            </Link>
          ))}
          {hasMore && (
            <div style={{ textAlign: 'center', padding: '12px' }}>
              <button
                className="secondary"
                disabled={loadingMore}
                onClick={loadMore}
              >
                {loadingMore ? '加载中...' : '加载更多'}
              </button>
            </div>
          )}
        </>
      )}
    </div>
  )
}
