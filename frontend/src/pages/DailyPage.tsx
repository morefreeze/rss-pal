import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getDailyDigest, DailyDigest } from '../api/client'
import ReadingMeta from '../components/ReadingMeta'
import BriefingTabs from '../components/BriefingTabs'
import { toast } from '../utils/toast'

function shiftDay(date: string, days: number): string {
  const d = new Date(date + 'T00:00:00+08:00')
  d.setDate(d.getDate() + days)
  const shanghai = new Date(d.getTime() + 8 * 3600 * 1000)
  return shanghai.toISOString().slice(0, 10)
}

export default function DailyPage() {
  const [digest, setDigest] = useState<DailyDigest | null>(null)
  const [loading, setLoading] = useState(true)
  const [date, setDate] = useState<string | undefined>(undefined)

  useEffect(() => { load(date) }, [date])

  const load = async (d?: string) => {
    setLoading(true)
    try {
      const data = await getDailyDigest(d)
      setDigest(data)
    } catch (err: any) {
      toast.error(err?.response?.data?.error || '加载日报失败')
    } finally {
      setLoading(false)
    }
  }

  if (loading) return (
    <div>
      <BriefingTabs current="daily" />
      <div className="card">加载中…</div>
    </div>
  )
  if (!digest) return (
    <div>
      <BriefingTabs current="daily" />
      <div className="card">暂无数据</div>
    </div>
  )

  const headerTitle = digest.mode === 'live' ? `今日精选 · ${digest.shown_date}（收集中）` : `本日精选 · ${digest.shown_date}`
  const showStaleTag = digest.pending && digest.shown_date !== digest.requested_date

  return (
    <div>
      <BriefingTabs current="daily" />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <h2 style={{ margin: 0 }}>{headerTitle}</h2>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="secondary" title="前一天" onClick={() => setDate(shiftDay(digest.shown_date, -1))}>‹ 前一天</button>
          <button className="secondary" title="后一天" onClick={() => setDate(shiftDay(digest.shown_date, 1))}>后一天 ›</button>
          {date !== undefined && (
            <button className="secondary" title="回到昨天" onClick={() => setDate(undefined)}>昨天</button>
          )}
        </div>
      </div>

      {showStaleTag && (
        <div className="card text-muted" style={{ marginBottom: 12, fontSize: 13 }}>
          {digest.requested_date} 的日报还在生成中,先看 {digest.shown_date} 的。
        </div>
      )}

      {digest.mode === 'live' && (
        <div className="card text-muted" style={{ marginBottom: 12, fontSize: 13 }}>
          今日还在收集中,明早 5 点后生成正式日报(含 AI 导语)。
        </div>
      )}

      {digest.pending && digest.articles.length === 0 ? (
        <div className="card">日报生成中,稍后刷新…</div>
      ) : (
        <>
          {digest.intro_text ? (
            <div className="card" style={{ marginBottom: 16, lineHeight: 1.7 }}>
              {digest.intro_text}
            </div>
          ) : digest.mode === 'cached' ? (
            <div className="card text-muted" style={{ marginBottom: 16, fontSize: 13 }}>
              本日导语生成失败或暂未生成,以下是入选文章:
            </div>
          ) : null}

          {digest.articles.length === 0 ? (
            <div className="card text-muted">当日无候选文章。</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              {digest.articles.map(a => (
                <Link key={a.id} to={`/articles/${a.id}`} className="card" style={{ display: 'block', textDecoration: 'none', color: 'inherit' }}>
                  <div style={{ fontSize: 15, fontWeight: 600, marginBottom: 4 }}>{a.title}</div>
                  <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', marginBottom: 6 }}>
                    {a.feed_title && <span className="text-muted text-sm">{a.feed_title}</span>}
                    {a.published_at && <span className="text-muted text-sm">{new Date(a.published_at).toLocaleDateString('zh-CN')}</span>}
                    <ReadingMeta wordCount={a.word_count} readingMinutes={a.reading_minutes} />
                  </div>
                  {a.summary_brief && <div className="text-muted" style={{ fontSize: 13, lineHeight: 1.5 }}>{a.summary_brief}</div>}
                </Link>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
