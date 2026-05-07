import { useNavigate } from 'react-router-dom'
import type { RecommendationDirection, RecArticleMeta } from '../api/client'

interface Props {
  recommendations: RecommendationDirection[]
  articles: Record<string, RecArticleMeta>
}

const KIND_LABEL: Record<string, string> = {
  core: '强化你的核心兴趣',
  emerging: '可能的新兴趣点',
}
const KIND_COLOR: Record<string, string> = {
  core: '#1a56db',
  emerging: '#7c3aed',
}

export default function RecommendationsCard({ recommendations, articles }: Props) {
  const navigate = useNavigate()
  if (!recommendations || recommendations.length === 0) return null

  const visibleDirs = recommendations
    .map(d => ({ ...d, articles: d.articles.filter(a => articles[String(a.article_id)]) }))
    .filter(d => d.articles.length > 0)
  if (visibleDirs.length === 0) return null

  return (
    <div className="card">
      <h3 className="mb-2">📍 为你推荐</h3>
      {visibleDirs.map((d, i) => (
        <div key={i} style={{ marginBottom: 16 }}>
          <div
            style={{
              fontWeight: 600,
              color: KIND_COLOR[d.direction_kind] || '#1a56db',
              marginBottom: 8,
              fontSize: 14,
            }}
          >
            ▸ {KIND_LABEL[d.direction_kind] || ''}：{d.direction}
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {d.articles.map(a => {
              const meta = articles[String(a.article_id)]
              return (
                <div
                  key={a.article_id}
                  onClick={() => navigate(`/articles/${a.article_id}`)}
                  style={{
                    padding: 12,
                    border: '1px solid #e5e7eb',
                    borderRadius: 8,
                    cursor: 'pointer',
                    background: '#fafafa',
                    transition: 'background 0.1s',
                  }}
                  onMouseEnter={e => (e.currentTarget.style.background = '#f3f4f6')}
                  onMouseLeave={e => (e.currentTarget.style.background = '#fafafa')}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
                    <span style={{ fontWeight: 500 }}>{meta.title}</span>
                    {meta.is_read && (
                      <span
                        style={{
                          fontSize: 11,
                          color: '#9ca3af',
                          background: '#e5e7eb',
                          padding: '2px 8px',
                          borderRadius: 10,
                          marginLeft: 8,
                          flexShrink: 0,
                        }}
                      >
                        已读过
                      </span>
                    )}
                  </div>
                  <div className="text-muted text-sm" style={{ marginTop: 4 }}>
                    {meta.feed_title}
                    {meta.brief ? ` · ${meta.brief}` : ''}
                  </div>
                  <div style={{ marginTop: 6, fontSize: 13, color: '#374151' }}>💡 {a.reason}</div>
                </div>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}
