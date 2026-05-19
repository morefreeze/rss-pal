import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { getLinkSetRecommendations, Article } from '../api/client'

export default function RecommendedPage() {
  const navigate = useNavigate()
  const [linkSetRecs, setLinkSetRecs] = useState<Article[]>([])
  const [loading, setLoading] = useState(true)
  const [showHelp, setShowHelp] = useState(false)

  useEffect(() => {
    getLinkSetRecommendations(7, 20)
      .then(setLinkSetRecs)
      .catch(() => setLinkSetRecs([]))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <div className="card">加载中…</div>

  if (linkSetRecs.length === 0) {
    return <div className="card text-muted">暂无推荐文章。订阅含链接合集的源(如 Hacker Newsletter)后,系统会自动展开并推荐其中的文章。</div>
  }

  return (
    <section className="mb-8">
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
        <h2 className="text-lg font-semibold" style={{ margin: 0 }}>本周精选 link_set 链接</h2>
        <button
          onClick={() => setShowHelp((v) => !v)}
          aria-label="说明"
          aria-expanded={showHelp}
          aria-controls="link-set-help"
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
        <div id="link-set-help" className="card text-sm" style={{ background: 'var(--surface-hover)', marginBottom: 12 }}>
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
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation()
                    navigate(`/articles/${a.parent_article_id}`)
                  }}
                  style={{
                    background: 'none',
                    border: 'none',
                    padding: 0,
                    color: 'var(--accent)',
                    cursor: 'pointer',
                    font: 'inherit',
                  }}
                >
                  {a.parent_title}
                </button>
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
  )
}
