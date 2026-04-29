import { useState, useEffect } from 'react'
import { getStats, getFetchProgress, FeedStats, FetchProgress } from '../api/client'

export default function StatsPage() {
  const [stats, setStats] = useState<FeedStats | null>(null)
  const [progress, setProgress] = useState<FetchProgress[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const loadData = async () => {
      try {
        const [statsData, progressData] = await Promise.all([
          getStats(),
          getFetchProgress()
        ])
        setStats(statsData)
        setProgress(progressData || [])
      } finally {
        setLoading(false)
      }
    }
    loadData()
  }, [])

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return '从未'
    return new Date(dateStr).toLocaleString('zh-CN')
  }

  if (loading) return <div className="card">Loading...</div>

  // Calculate overall progress
  const totalArticles = progress.reduce((sum, f) => sum + f.article_count, 0)
  const totalWithContent = progress.reduce((sum, f) => sum + Math.round(f.article_count * f.content_progress / 100), 0)
  const totalWithSummary = progress.reduce((sum, f) => sum + Math.round(f.article_count * f.summary_progress / 100), 0)

  const contentEfficiency = totalArticles > 0 ? Math.round(totalWithContent / totalArticles * 100) : 0
  const summaryEfficiency = totalArticles > 0 ? Math.round(totalWithSummary / totalArticles * 100) : 0

  // Estimate completion time (assuming 2 articles per minute for content fetch, 1 per minute for summary)
  const remainingContent = totalArticles - totalWithContent
  const remainingSummary = totalArticles - totalWithSummary
  const estimatedMinutes = Math.max(remainingContent / 2, remainingSummary / 1)
  const estimatedTime = estimatedMinutes < 60
    ? `${Math.round(estimatedMinutes)} 分钟`
    : `${Math.round(estimatedMinutes / 60)} 小时 ${Math.round(estimatedMinutes % 60)} 分钟`

  return (
    <div>
      {/* Overview Stats */}
      <div className="card">
        <h2>概览</h2>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: '1rem' }}>
          <div>
            <div className="text-muted">订阅源</div>
            <div style={{ fontSize: '2rem', fontWeight: 'bold' }}>{stats?.total_feeds || 0}</div>
            <div className="text-muted">活跃: {stats?.active_feeds || 0}</div>
          </div>
          <div>
            <div className="text-muted">文章总数</div>
            <div style={{ fontSize: '2rem', fontWeight: 'bold' }}>{stats?.total_articles || 0}</div>
            <div className="text-muted">今日: {stats?.today_articles || 0}</div>
          </div>
          <div>
            <div className="text-muted">内容抓取</div>
            <div style={{ fontSize: '2rem', fontWeight: 'bold' }}>{contentEfficiency}%</div>
            <div className="text-muted">{totalWithContent} / {totalArticles}</div>
          </div>
          <div>
            <div className="text-muted">摘要生成</div>
            <div style={{ fontSize: '2rem', fontWeight: 'bold' }}>{summaryEfficiency}%</div>
            <div className="text-muted">{totalWithSummary} / {totalArticles}</div>
          </div>
        </div>
      </div>

      {/* Efficiency & Estimates */}
      <div className="card">
        <h2>抓取效率</h2>
        <div style={{ marginBottom: '1rem' }}>
          <div className="flex-between mb-1">
            <span>内容抓取进度</span>
            <span>{totalWithContent} / {totalArticles}</span>
          </div>
          <div style={{ height: 20, backgroundColor: '#e0e0e0', borderRadius: 4, overflow: 'hidden' }}>
            <div style={{
              height: '100%',
              width: `${contentEfficiency}%`,
              backgroundColor: '#0066cc',
              transition: 'width 0.3s ease'
            }} />
          </div>
        </div>
        <div style={{ marginBottom: '1rem' }}>
          <div className="flex-between mb-1">
            <span>摘要生成进度</span>
            <span>{totalWithSummary} / {totalArticles}</span>
          </div>
          <div style={{ height: 20, backgroundColor: '#e0e0e0', borderRadius: 4, overflow: 'hidden' }}>
            <div style={{
              height: '100%',
              width: `${summaryEfficiency}%`,
              backgroundColor: '#22c55e',
              transition: 'width 0.3s ease'
            }} />
          </div>
        </div>
        <div className="text-muted">
          预计完成时间: {estimatedTime}
        </div>
      </div>

      {/* Per-Feed Progress */}
      <div className="card">
        <h2>订阅源详情</h2>
        {progress.length === 0 ? (
          <div className="text-muted">暂无活跃订阅源</div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid #ddd' }}>
                  <th style={{ textAlign: 'left', padding: '0.5rem' }}>订阅源</th>
                  <th style={{ textAlign: 'center', padding: '0.5rem' }}>文章数</th>
                  <th style={{ textAlign: 'center', padding: '0.5rem' }}>内容</th>
                  <th style={{ textAlign: 'center', padding: '0.5rem' }}>摘要</th>
                  <th style={{ textAlign: 'left', padding: '0.5rem' }}>最后抓取</th>
                </tr>
              </thead>
              <tbody>
                {progress.map((feed) => (
                  <tr key={feed.feed_id} style={{ borderBottom: '1px solid #eee' }}>
                    <td style={{ padding: '0.5rem' }}>
                      <div style={{ fontWeight: 500 }}>{feed.feed_title}</div>
                      <div className="text-muted text-sm">{feed.feed_url}</div>
                    </td>
                    <td style={{ textAlign: 'center', padding: '0.5rem' }}>{feed.article_count}</td>
                    <td style={{ textAlign: 'center', padding: '0.5rem' }}>
                      <span style={{
                        padding: '2px 8px',
                        borderRadius: 4,
                        backgroundColor: feed.content_progress >= 80 ? '#dcfce7' : feed.content_progress >= 50 ? '#fef3c7' : '#fee2e2',
                        color: feed.content_progress >= 80 ? '#166534' : feed.content_progress >= 50 ? '#92400e' : '#991b1b'
                      }}>
                        {feed.content_progress}%
                      </span>
                    </td>
                    <td style={{ textAlign: 'center', padding: '0.5rem' }}>
                      <span style={{
                        padding: '2px 8px',
                        borderRadius: 4,
                        backgroundColor: feed.summary_progress >= 80 ? '#dcfce7' : feed.summary_progress >= 50 ? '#fef3c7' : '#fee2e2',
                        color: feed.summary_progress >= 80 ? '#166534' : feed.summary_progress >= 50 ? '#92400e' : '#991b1b'
                      }}>
                        {feed.summary_progress}%
                      </span>
                    </td>
                    <td style={{ padding: '0.5rem' }} className="text-muted text-sm">{formatDate(feed.last_fetched_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
