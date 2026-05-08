import { useState, useEffect } from 'react'
import { getFeedHealth, FeedHealthResponse } from '../api/client'
import { Link } from 'react-router-dom'
import FeedHealthKPI from '../components/FeedHealthKPI'
import FeedHealthTable from '../components/FeedHealthTable'

const WINDOW_KEY = 'feedHealthWindow'

export default function FeedHealthPage() {
  const [timeWindow, setTimeWindow] = useState<'30d' | '90d'>(() => {
    const saved = localStorage.getItem(WINDOW_KEY)
    return saved === '90d' ? '90d' : '30d'
  })
  const [data, setData] = useState<FeedHealthResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    setLoading(true)
    setError('')
    getFeedHealth(timeWindow)
      .then(setData)
      .catch((err) => setError(err?.response?.data?.error || '加载失败'))
      .finally(() => setLoading(false))
    localStorage.setItem(WINDOW_KEY, timeWindow)
  }, [timeWindow])

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <h1 style={{ margin: 0 }}>Feed 健康度</h1>
        <Link to="/feeds">← 订阅源管理</Link>
      </div>
      <div style={{ marginBottom: 16 }}>
        时间窗口：
        <button
          onClick={() => setTimeWindow('30d')}
          style={{ marginLeft: 8, fontWeight: timeWindow === '30d' ? 'bold' : 'normal' }}
        >30 天</button>
        <button
          onClick={() => setTimeWindow('90d')}
          style={{ marginLeft: 4, fontWeight: timeWindow === '90d' ? 'bold' : 'normal' }}
        >90 天</button>
      </div>
      {loading && <div>加载中…</div>}
      {error && <div className="error">{error}</div>}
      {data && (
        <>
          <FeedHealthKPI kpi={data.kpi} window={data.window} />
          <FeedHealthTable rows={data.rows} onChange={() => {
            // refetch
            setLoading(true)
            getFeedHealth(timeWindow).then(setData).finally(() => setLoading(false))
          }} />
        </>
      )}
    </div>
  )
}
