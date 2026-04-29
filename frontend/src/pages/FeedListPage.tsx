import { useState, useEffect } from 'react'
import { getFeeds, addFeed, deleteFeed, fetchFeedNow, createInviteCode, getInviteCodes, Feed, InviteCode } from '../api/client'

interface FeedListPageProps {
  user: { id: number; username: string; is_admin: boolean } | null
}

export default function FeedListPage({ user }: FeedListPageProps) {
  const [feeds, setFeeds] = useState<Feed[]>([])
  const [newUrl, setNewUrl] = useState('')
  const [loading, setLoading] = useState(true)
  const [fetchingId, setFetchingId] = useState<number | null>(null)
  const [inviteCodes, setInviteCodes] = useState<InviteCode[]>([])
  const [showInvitePanel, setShowInvitePanel] = useState(false)
  const [newCode, setNewCode] = useState('')

  useEffect(() => {
    loadFeeds()
  }, [])

  const loadFeeds = async () => {
    try {
      const data = await getFeeds()
      setFeeds(data || [])
    } finally {
      setLoading(false)
    }
  }

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newUrl) return

    try {
      const feed = await addFeed(newUrl)
      setNewUrl('')
      await loadFeeds()
      // Auto-fetch after adding
      try {
        await fetchFeedNow(feed.id)
        await loadFeeds()
      } catch {
        // Fetch failed silently, worker will pick it up later
      }
    } catch (err) {
      alert('添加失败')
    }
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
    const date = new Date(dateStr)
    return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  const loadInviteCodes = async () => {
    try {
      const data = await getInviteCodes()
      setInviteCodes(data || [])
    } catch { /* ignore */ }
  }

  const handleCreateCode = async () => {
    try {
      const code = await createInviteCode(72)
      setNewCode(code.code)
      loadInviteCodes()
    } catch {
      alert('创建失败')
    }
  }

  if (loading) return <div className="card">Loading...</div>

  return (
    <div>
      <div className="flex-between mb-2">
        <h2>订阅管理</h2>
        {user?.is_admin && (
          <button className="secondary" onClick={() => { setShowInvitePanel(!showInvitePanel); if (!showInvitePanel) loadInviteCodes() }}>
            邀请码
          </button>
        )}
      </div>

      {showInvitePanel && (
        <div className="card mb-2">
          <div className="flex-between mb-1">
            <h3>邀请码管理</h3>
            <button onClick={handleCreateCode}>生成新邀请码</button>
          </div>
          {newCode && (
            <div className="card" style={{ background: '#f0f9ff', marginBottom: 8, padding: '8px 12px' }}>
              新邀请码: <strong>{newCode}</strong>
              <button className="secondary text-sm" style={{ marginLeft: 8 }} onClick={() => navigator.clipboard.writeText(newCode)}>复制</button>
            </div>
          )}
          {inviteCodes.length === 0 ? (
            <div className="text-muted text-sm">暂无邀请码</div>
          ) : (
            inviteCodes.map(ic => (
              <div key={ic.id} className="flex-between text-sm" style={{ padding: '4px 0' }}>
                <span>
                  <strong>{ic.code}</strong>
                  {ic.used_by ? <span className="text-muted"> (已使用)</span> : <span style={{ color: '#16a34a' }}> (可用)</span>}
                </span>
                <span className="text-muted">
                  {ic.expires_at ? `有效期至 ${new Date(ic.expires_at).toLocaleDateString('zh-CN')}` : '永久'}
                </span>
              </div>
            ))
          )}
        </div>
      )}

      <form onSubmit={handleAdd} className="flex gap-2 mb-2">
        <input
          type="url"
          placeholder="RSS 地址"
          value={newUrl}
          onChange={e => setNewUrl(e.target.value)}
          style={{ flex: 1 }}
        />
        <button type="submit">添加</button>
      </form>

      {feeds.length === 0 ? (
        <div className="card text-muted">暂无订阅</div>
      ) : (
        feeds.map(feed => (
          <div key={feed.id} className="card">
            <div className="flex-between">
              <div>
                <div className="text-bold">{feed.title || feed.url}</div>
                <div className="text-muted text-sm">{feed.url}</div>
                <div className="text-muted text-sm mt-1">
                  {feed.owner_id ? '私有' : '共享'} | 上次抓取: {formatDate(feed.last_fetched_at)}
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
