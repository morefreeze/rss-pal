import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { getRecommendedFeeds, subscribeRecommendedFeed, getLinkSetRecommendations, RecommendedFeed, Article } from '../api/client'
import { toast } from '../utils/toast'
import { CATEGORY_LABELS, CATEGORY_ORDER } from '../components/categoryLabels'

export default function RecommendedPage() {
  const navigate = useNavigate()
  const [items, setItems] = useState<RecommendedFeed[]>([])
  const [loading, setLoading] = useState(true)
  const [busyId, setBusyId] = useState<number | null>(null)
  const [linkSetRecs, setLinkSetRecs] = useState<Article[]>([])
  const [showHelp, setShowHelp] = useState(false)

  useEffect(() => {
    getLinkSetRecommendations(7, 20)
      .then(setLinkSetRecs)
      .catch(() => setLinkSetRecs([]))
  }, [])

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
      {linkSetRecs.length > 0 && (
        <section className="mb-8">
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
            <h2 className="text-lg font-semibold" style={{ margin: 0 }}>本周精选 link_set 链接</h2>
            <button
              onClick={() => setShowHelp((v) => !v)}
              aria-label="说明"
              aria-expanded={showHelp}
              style={{
                background: 'none',
                border: 'none',
                cursor: 'pointer',
                fontSize: 16,
                padding: 0,
                lineHeight: 1,
              }}
            >
              ℹ️
            </button>
          </div>
          {showHelp && (
            <div className="card text-sm" style={{ background: 'var(--surface-hover)', marginBottom: 12 }}>
              <p style={{ marginTop: 0 }}>
                这里的文章来自你订阅源里"内含链接合集"的文章(如 Hacker Newsletter)。系统会自动展开链接、抓取正文,按以下规则推荐:
              </p>
              <ol style={{ paddingLeft: 20, marginBottom: 8 }}>
                <li>优先按你的偏好(过去 30 天 like / save / 收听时长加权)排序</li>
                <li>没有偏好数据时按编辑加权 + 发布时间排序,保证质量</li>
                <li>已读完的文章默认不出现,但当合格文章不足时会作为兜底补齐(会标注"兜底推荐")</li>
              </ol>
              <p className="text-muted" style={{ marginBottom: 0 }}>
                如果某期 newsletter 没出现,可能是该订阅源还未被系统识别为"含链接合集",或该期所有文章都已读完。
              </p>
            </div>
          )}
          <div className="space-y-3">
            {linkSetRecs.map((a) => (
              <div
                key={a.id}
                className="card"
                style={{ cursor: 'pointer' }}
                onClick={() => navigate(`/articles/${a.id}`)}
              >
                <div className="text-bold">{a.title}</div>
                {a.summary_brief && (
                  <div className="text-muted text-sm mt-1">{a.summary_brief.slice(0, 120)}…</div>
                )}
                {a.feed_title && (
                  <div className="text-muted text-sm mt-1" style={{ color: 'var(--accent)' }}>{a.feed_title}</div>
                )}
                {a.parent_title && a.parent_article_id != null && (
                  <div className="text-muted text-sm mt-1">
                    来自《
                    <span
                      onClick={(e) => {
                        e.stopPropagation()
                        navigate(`/articles/${a.parent_article_id}`)
                      }}
                      style={{ color: 'var(--accent)', cursor: 'pointer' }}
                    >
                      {a.parent_title}
                    </span>
                    》
                    {a.is_fallback && (
                      <span className="text-muted" style={{ marginLeft: 8, fontSize: 11 }}>
                        · 兜底推荐(可能已读过)
                      </span>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        </section>
      )}
      <h2 style={{ marginBottom: 16 }}>推荐订阅</h2>
      <p className="text-muted text-sm" style={{ marginBottom: 16 }}>
        以下是预置的优质订阅源,按内容方向分类。点击「订阅」加入你的订阅列表。
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
                  <span style={{ fontSize: 11, padding: '2px 6px', background: 'var(--surface-hover)', borderRadius: 4 }}>
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
