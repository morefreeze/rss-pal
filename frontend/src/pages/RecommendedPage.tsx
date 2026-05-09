import { useEffect, useState } from 'react'
import { getRecommendedFeeds, subscribeRecommendedFeed, RecommendedFeed } from '../api/client'
import { toast } from '../utils/toast'

const CATEGORY_LABELS: Record<string, string> = {
  ai_eng: 'AI 工程',
  ai: 'AI',
  cn_tech: '中文科技',
  enterprise: '企业基建',
  podcast: '播客',
  youtube: '视频',
}
const CATEGORY_ORDER = ['ai_eng', 'ai', 'cn_tech', 'enterprise', 'youtube', 'podcast']

export default function RecommendedPage() {
  const [items, setItems] = useState<RecommendedFeed[]>([])
  const [loading, setLoading] = useState(true)
  const [busyId, setBusyId] = useState<number | null>(null)

  useEffect(() => { load() }, [])

  const load = async () => {
    setLoading(true)
    try {
      const data = await getRecommendedFeeds()
      setItems(data)
    } finally {
      setLoading(false)
    }
  }

  const handleSubscribe = async (id: number) => {
    setBusyId(id)
    try {
      await subscribeRecommendedFeed(id)
      await load()
    } catch (err: any) {
      toast.error(err?.response?.data?.error || '订阅失败')
    } finally {
      setBusyId(null)
    }
  }

  if (loading) return <div className="card">加载中…</div>

  const grouped: Record<string, RecommendedFeed[]> = {}
  for (const it of items) {
    if (!grouped[it.category]) grouped[it.category] = []
    grouped[it.category].push(it)
  }

  return (
    <div>
      <h2 style={{ marginBottom: 16 }}>推荐订阅</h2>
      <p className="text-muted text-sm" style={{ marginBottom: 16 }}>
        以下是从 bestblogs.dev 精选的高质量来源。点击「订阅」即可添加到你的订阅列表。
      </p>
      {CATEGORY_ORDER.filter(c => grouped[c]?.length).map(cat => (
        <section key={cat} style={{ marginBottom: 24 }}>
          <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 8 }}>
            {CATEGORY_LABELS[cat] || cat}
          </h3>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 12 }}>
            {grouped[cat].map(rf => (
              <div key={rf.id} className="card" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                  <strong style={{ fontSize: 14 }}>{rf.title}</strong>
                  <span style={{ fontSize: 11, padding: '2px 6px', background: '#f0f0f0', borderRadius: 4 }}>
                    {rf.language === 'zh' ? '中文' : 'English'}
                  </span>
                </div>
                {rf.description && (
                  <div className="text-muted text-sm" style={{ fontSize: 12 }}>{rf.description}</div>
                )}
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 'auto' }}>
                  {rf.is_broken ? (
                    <span style={{ fontSize: 12, color: '#c33' }}>⚠ 当前路由不可用</span>
                  ) : rf.subscribed ? (
                    <span style={{ fontSize: 12, color: '#28a745' }}>✓ 已订阅</span>
                  ) : (
                    <button
                      onClick={() => handleSubscribe(rf.id)}
                      disabled={busyId === rf.id}
                      style={{ padding: '4px 10px', fontSize: 12 }}
                    >
                      {busyId === rf.id ? '订阅中…' : '订阅'}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </section>
      ))}
      {items.length === 0 && (
        <div className="card text-muted">暂无推荐源,请等待管理员配置。</div>
      )}
    </div>
  )
}
