import { useState, useEffect, useRef } from 'react'
import { getFeeds, addFeed, deleteFeed, fetchFeedNow, previewFeed, toggleFeedActive, exportOPML, Feed, FeedPreview } from '../api/client'
import { toast } from '../utils/toast'

const POPULAR_FEEDS = [
  { name: 'Hacker News', url: 'https://hnrss.org/frontpage', desc: '全球科技社区热帖聚合' },
  { name: '36氪', url: 'https://36kr.com/feed', desc: '中国科技商业资讯聚合' },
  { name: '少数派', url: 'https://sspai.com/feed', desc: '数字生活方式与效率工具' },
  { name: 'BBC 中文', url: 'https://feeds.bbci.co.uk/zhongwen/simp/rss.xml', desc: '国际新闻中文报道' },
  { name: 'The Verge', url: 'https://www.theverge.com/rss/index.xml', desc: '英文科技新闻聚合' },
]

export default function FeedListPage() {
  const [feeds, setFeeds] = useState<Feed[]>([])
  const [newUrl, setNewUrl] = useState('')
  const [loading, setLoading] = useState(true)
  const [fetchingId, setFetchingId] = useState<number | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [previewStatus, setPreviewStatus] = useState('')
  const [preview, setPreview] = useState<FeedPreview | null>(null)
  const [previewError, setPreviewError] = useState('')
  const [adding, setAdding] = useState(false)
  const [addSuccess, setAddSuccess] = useState('')
  const [importing, setImporting] = useState(false)
  const previewTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => { loadFeeds() }, [])

  const loadFeeds = async () => {
    try {
      const data = await getFeeds()
      setFeeds(data || [])
    } finally {
      setLoading(false)
    }
  }

  const normalizeURL = (raw: string) => {
    const trimmed = raw.trim()
    if (trimmed && !trimmed.startsWith('http://') && !trimmed.startsWith('https://')) {
      return 'https://' + trimmed
    }
    return trimmed
  }

  const doPreview = async (url: string) => {
    const normalized = normalizeURL(url)
    if (!normalized) return
    setNewUrl(normalized)
    setPreviewing(true)
    setPreviewStatus('获取中...')
    setPreview(null)
    setPreviewError('')
    // After 4s show "probing RSS" hint so user knows it's still working
    if (previewTimer.current) clearTimeout(previewTimer.current)
    previewTimer.current = setTimeout(() => setPreviewStatus('正在探测 RSS 地址...'), 4000)
    try {
      const result = await previewFeed(normalized)
      setPreview(result)
    } catch (err: any) {
      setPreviewError(err?.response?.data?.error || '无法获取该地址的内容，请检查 URL 是否正确')
    } finally {
      if (previewTimer.current) clearTimeout(previewTimer.current)
      setPreviewing(false)
      setPreviewStatus('')
    }
  }

  const handlePreview = async (e: React.FormEvent) => {
    e.preventDefault()
    doPreview(newUrl)
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
    } catch (err: any) {
      toast.error(err?.response?.data?.error || '添加失败，请重试')
    } finally {
      setAdding(false)
    }
  }

  const handleCancelPreview = () => {
    setPreview(null)
    setPreviewError('')
  }

  const handleToggleActive = async (feed: Feed) => {
    try {
      await toggleFeedActive(feed.id, !feed.is_active, feed.title || feed.url)
      setFeeds(prev => prev.map(f => f.id === feed.id ? { ...f, is_active: !f.is_active } : f))
    } catch {
      toast.error('操作失败')
    }
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确定删除此订阅？')) return
    try {
      await deleteFeed(id)
      loadFeeds()
    } catch {
      toast.error('删除失败')
    }
  }

  const handleFetch = async (id: number) => {
    setFetchingId(id)
    try {
      const result = await fetchFeedNow(id)
      await loadFeeds()
      toast.success(`${result.feed_title || '订阅源'}：抓取完成，${result.new_articles} 篇新文章`)
    } catch (err: any) {
      toast.error('抓取失败：' + (err?.response?.data?.error || err?.message || '未知错误'))
    } finally {
      setFetchingId(null)
    }
  }

  const handleExportOPML = async () => {
    try {
      const blob = await exportOPML()
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = 'rss-pal-subscriptions.opml'
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(a.href)
    } catch {
      toast.error('导出失败')
    }
  }

  const handleImportOPML = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    e.target.value = ''
    setImporting(true)
    try {
      const text = await file.text()
      const parser = new DOMParser()
      const doc = parser.parseFromString(text, 'text/xml')
      const outlines = doc.querySelectorAll('outline[xmlUrl]')
      const entries = Array.from(outlines)
        .map(o => ({ url: o.getAttribute('xmlUrl') || '', type: o.getAttribute('type') || 'rss' }))
        .filter(e => !!e.url)
      if (entries.length === 0) { toast.error('未找到有效的订阅地址'); return }
      let added = 0, skipped = 0
      for (const entry of entries) {
        try { await addFeed(entry.url, entry.type === 'html' ? 'html' : 'rss'); added++ } catch { skipped++ }
      }
      await loadFeeds()
      toast.success(`导入完成：${added} 个订阅添加成功${skipped > 0 ? `，${skipped} 个已存在或失败` : ''}`)
    } catch {
      toast.error('OPML 文件解析失败')
    } finally {
      setImporting(false)
    }
  }

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return '从未'
    return new Date(dateStr).toLocaleDateString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  if (loading) return <div className="card">Loading...</div>

  return (
    <div>
      <div className="flex-between mb-2">
        <h2>订阅管理</h2>
        <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          <input id="opml-import" type="file" accept=".opml,.xml" style={{ display: 'none' }} onChange={handleImportOPML} />
          <button className="secondary" style={{ fontSize: 12, padding: '3px 10px' }} disabled={importing} onClick={() => (document.getElementById('opml-import') as HTMLInputElement)?.click()}>
            {importing ? '导入中...' : '导入 OPML'}
          </button>
          <button className="secondary" style={{ fontSize: 12, padding: '3px 10px' }} onClick={handleExportOPML}>
            导出 OPML
          </button>
        </div>
      </div>

      {addSuccess && (
        <div style={{ background: '#f0fdf4', border: '1px solid #bbf7d0', borderRadius: 6, padding: '10px 14px', marginBottom: 12, color: '#166534', fontSize: 14 }}>
          ✓ {addSuccess}
        </div>
      )}

      {/* Add feed: 2-step preview flow */}
      <div className="card mb-2">
        <h3 className="mb-2">添加订阅</h3>
        <p className="text-muted text-sm mb-2">支持 RSS/Atom 地址，也可直接输入任意博客或新闻网站 URL，系统自动提取文章列表，预览确认后再订阅</p>

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
            {previewing ? previewStatus || '获取中...' : '预览'}
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
                onClick={() => { setNewUrl(f.url); doPreview(f.url) }}
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
                  · {(preview.items ?? []).length} 篇文章
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
              {(preview.items ?? []).length === 0 ? (
                <div className="text-muted text-sm">
                  未找到文章。可能原因：该页面使用 JavaScript 动态加载内容，或此地址不是文章列表页。
                  <br />建议尝试该网站的 RSS 直接地址（通常在页脚或设置中可找到）。
                </div>
              ) : (
                (preview.items ?? []).map((item, i) => (
                  <div key={i} style={{ padding: '5px 0', borderBottom: i < (preview.items ?? []).length - 1 ? '1px solid #f5f5f5' : 'none' }}>
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
                <div className="text-bold" style={!feed.is_active ? { color: '#aaa' } : {}}>
                  {feed.title || feed.url}
                  {feed.feed_type === 'html' && (
                    <span className="text-sm" style={{ marginLeft: 6, padding: '1px 6px', background: '#fef9c3', borderRadius: 4, color: '#854d0e' }}>网页</span>
                  )}
                  {!feed.is_active && (
                    <span className="text-sm" style={{ marginLeft: 6, padding: '1px 6px', background: '#f3f4f6', borderRadius: 4, color: '#6b7280' }}>已暂停</span>
                  )}
                </div>
                <div className="text-muted text-sm">{feed.url}</div>
                <div className="text-muted text-sm mt-1">
                  {feed.owner_id ? '私有' : '共享'} · {feed.article_count} 篇
                  {feed.unread_count > 0 && <span style={{ color: '#2563eb', fontWeight: 500 }}> · {feed.unread_count} 未读</span>}
                  {' '}· 上次抓取：{formatDate(feed.last_fetched_at)}
                </div>
              </div>
              <div className="flex gap-1">
                {feed.is_active ? (
                  <button
                    className="secondary"
                    disabled={fetchingId === feed.id}
                    onClick={() => handleFetch(feed.id)}
                  >
                    {fetchingId === feed.id ? '抓取中...' : '刷新'}
                  </button>
                ) : null}
                <button
                  className="secondary"
                  onClick={() => handleToggleActive(feed)}
                  style={!feed.is_active ? { color: '#92400e', background: '#fef9c3' } : {}}
                >
                  {feed.is_active ? '暂停' : '继续'}
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
