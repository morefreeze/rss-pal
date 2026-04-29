import { useState, useEffect } from 'react'
import { getFeeds, addFeed, deleteFeed, fetchFeedNow, previewFeed, Feed, FeedPreview } from '../api/client'

const POPULAR_FEEDS = [
  { name: 'Hacker News', url: 'https://hnrss.org/frontpage', desc: '科技社区热帖' },
  { name: '少数派', url: 'https://sspai.com/feed', desc: '数字生活方式' },
  { name: 'V2EX', url: 'https://www.v2ex.com/index.xml', desc: '技术&创意社区' },
  { name: 'The Verge', url: 'https://www.theverge.com/rss/index.xml', desc: '科技新闻' },
  { name: '阮一峰博客', url: 'https://www.ruanyifeng.com/blog/atom.xml', desc: '技术&周刊' },
]

export default function FeedListPage() {
  const [feeds, setFeeds] = useState<Feed[]>([])
  const [newUrl, setNewUrl] = useState('')
  const [loading, setLoading] = useState(true)
  const [fetchingId, setFetchingId] = useState<number | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [preview, setPreview] = useState<FeedPreview | null>(null)
  const [previewError, setPreviewError] = useState('')
  const [adding, setAdding] = useState(false)
  const [addSuccess, setAddSuccess] = useState('')

  useEffect(() => { loadFeeds() }, [])

  const loadFeeds = async () => {
    try {
      const data = await getFeeds()
      setFeeds(data || [])
    } finally {
      setLoading(false)
    }
  }

  const handlePreview = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newUrl.trim()) return
    setPreviewing(true)
    setPreview(null)
    setPreviewError('')
    try {
      const result = await previewFeed(newUrl.trim())
      setPreview(result)
    } catch (err: any) {
      setPreviewError(err?.response?.data?.error || '无法获取该地址的内容，请检查 URL 是否正确')
    } finally {
      setPreviewing(false)
    }
  }

  const handleConfirmAdd = async () => {
    if (!preview) return
    setAdding(true)
    setAddSuccess('')
    try {
      const actualUrl = preview.actual_url || newUrl.trim()
      const feed = await addFeed(actualUrl, preview.feed_type)
      setNewUrl('')
      setPreview(null)
      await loadFeeds()
      // Auto-fetch after adding
      try {
        const result = await fetchFeedNow(feed.id)
        await loadFeeds()
        setAddSuccess(`已添加「${result.feed_title || feed.url}」，抓取到 ${result.new_articles} 篇新文章`)
      } catch {
        setAddSuccess('订阅已添加，后台将自动抓取文章')
      }
      setTimeout(() => setAddSuccess(''), 5000)
    } catch {
      alert('添加失败，请重试')
    } finally {
      setAdding(false)
    }
  }

  const handleCancelPreview = () => {
    setPreview(null)
    setPreviewError('')
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确定删除此订阅？')) return
    try {
      await deleteFeed(id)
      loadFeeds()
    } catch {
      alert('删除失败')
    }
  }

  const handleFetch = async (id: number) => {
    setFetchingId(id)
    try {
      const result = await fetchFeedNow(id)
      await loadFeeds()
      alert(`${result.feed_title || '订阅源'}：抓取完成，${result.new_articles} 篇新文章`)
    } catch (err: any) {
      alert('抓取失败：' + (err?.response?.data?.error || err?.message || '未知错误'))
    } finally {
      setFetchingId(null)
    }
  }

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return '从未'
    return new Date(dateStr).toLocaleDateString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  if (loading) return <div className="card">Loading...</div>

  return (
    <div>
      <h2 className="mb-2">订阅管理</h2>

      {addSuccess && (
        <div style={{ background: '#f0fdf4', border: '1px solid #bbf7d0', borderRadius: 6, padding: '10px 14px', marginBottom: 12, color: '#166534', fontSize: 14 }}>
          ✓ {addSuccess}
        </div>
      )}

      {/* Add feed: 2-step preview flow */}
      <div className="card mb-2">
        <h3 className="mb-2">添加订阅</h3>
        <p className="text-muted text-sm mb-2">支持 RSS/Atom 订阅地址，也可以直接输入博客或新闻网站地址，系统会自动识别</p>

        <form onSubmit={handlePreview} className="flex gap-2 mb-2">
          <input
            type="text"
            placeholder="输入 RSS 地址或网站 URL"
            value={newUrl}
            onChange={e => { setNewUrl(e.target.value); setPreview(null); setPreviewError('') }}
            style={{ flex: 1 }}
            disabled={previewing || adding}
          />
          <button type="submit" disabled={previewing || adding || !newUrl.trim()}>
            {previewing ? '获取中...' : '预览'}
          </button>
        </form>

        {/* Popular feeds */}
        <div className="mb-2">
          <div className="text-sm text-muted mb-1">热门推荐：</div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
            {POPULAR_FEEDS.map(f => (
              <button
                key={f.url}
                className="secondary"
                style={{ fontSize: 12, padding: '3px 10px' }}
                title={f.desc}
                onClick={() => { setNewUrl(f.url); setPreview(null); setPreviewError('') }}
              >
                {f.name}
              </button>
            ))}
          </div>
        </div>

        {/* Preview error */}
        {previewError && (
          <div style={{ color: '#dc2626', fontSize: 14, marginBottom: 8 }}>{previewError}</div>
        )}

        {/* Preview result */}
        {preview && (
          <div style={{ border: '1px solid #e0e0e0', borderRadius: 6, padding: 12 }}>
            <div className="flex-between mb-2">
              <div>
                <div className="text-bold">{preview.feed_title || '未命名订阅源'}</div>
                <div className="text-muted text-sm">
                  {preview.feed_type === 'html' ? '🌐 网页抓取模式' : '📡 RSS/Atom 订阅'}
                  {preview.actual_url !== newUrl.trim() && (
                    <span style={{ marginLeft: 6 }}>· 已自动发现 RSS 地址</span>
                  )}
                  · {preview.items.length} 篇文章
                </div>
              </div>
              <div className="flex gap-1">
                <button onClick={handleConfirmAdd} disabled={adding}>
                  {adding ? '添加中...' : '确认订阅'}
                </button>
                <button className="secondary" onClick={handleCancelPreview}>取消</button>
              </div>
            </div>
            <div>
              {preview.items.length === 0 ? (
                <div className="text-muted text-sm">未找到文章，该地址可能不包含可识别的内容</div>
              ) : (
                preview.items.map((item, i) => (
                  <div key={i} style={{ padding: '5px 0', borderBottom: i < preview.items.length - 1 ? '1px solid #f5f5f5' : 'none' }}>
                    <a href={item.url} target="_blank" rel="noopener noreferrer" className="text-sm" style={{ color: '#213547' }}>
                      {item.title}
                    </a>
                    {item.published_at && (
                      <span className="text-muted text-sm" style={{ marginLeft: 8 }}>
                        {new Date(item.published_at).toLocaleDateString('zh-CN')}
                      </span>
                    )}
                  </div>
                ))
              )}
            </div>
          </div>
        )}
      </div>

      {/* Existing feeds list */}
      {feeds.length === 0 ? (
        <div className="card text-muted">暂无订阅，从上方添加你的第一个订阅源</div>
      ) : (
        feeds.map(feed => (
          <div key={feed.id} className="card">
            <div className="flex-between">
              <div>
                <div className="text-bold">
                  {feed.title || feed.url}
                  {feed.feed_type === 'html' && (
                    <span className="text-sm" style={{ marginLeft: 6, padding: '1px 6px', background: '#fef9c3', borderRadius: 4, color: '#854d0e' }}>网页</span>
                  )}
                </div>
                <div className="text-muted text-sm">{feed.url}</div>
                <div className="text-muted text-sm mt-1">
                  {feed.owner_id ? '私有' : '共享'} · 上次抓取：{formatDate(feed.last_fetched_at)}
                </div>
              </div>
              <div className="flex gap-1">
                <button
                  className="secondary"
                  disabled={fetchingId === feed.id}
                  onClick={() => handleFetch(feed.id)}
                >
                  {fetchingId === feed.id ? '抓取中...' : '刷新'}
                </button>
                <button className="secondary" onClick={() => handleDelete(feed.id)}>删除</button>
              </div>
            </div>
          </div>
        ))
      )}
    </div>
  )
}
