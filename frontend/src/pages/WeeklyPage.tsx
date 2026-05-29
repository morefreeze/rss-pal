import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getWeeklyDigest, WeeklyDigest } from '../api/client'
import ReadingMeta from '../components/ReadingMeta'
import BriefingTabs from '../components/BriefingTabs'
import { toast } from '../utils/toast'

function shiftWeek(weekStart: string, days: number): string {
  const d = new Date(weekStart + 'T00:00:00+08:00')
  d.setDate(d.getDate() + days)
  const shanghai = new Date(d.getTime() + 8 * 3600 * 1000)
  return shanghai.toISOString().slice(0, 10)
}

export default function WeeklyPage() {
  const [digest, setDigest] = useState<WeeklyDigest | null>(null)
  const [loading, setLoading] = useState(true)
  const [week, setWeek] = useState<string | undefined>(undefined)

  useEffect(() => { load(week) }, [week])

  const load = async (w?: string) => {
    setLoading(true)
    try {
      const data = await getWeeklyDigest(w)
      setDigest(data)
    } catch (err: any) {
      toast.error(err?.response?.data?.error || '加载周刊失败')
    } finally {
      setLoading(false)
    }
  }

  if (loading) return (
    <div>
      <BriefingTabs current="weekly" />
      <div className="card">加载中…</div>
    </div>
  )
  if (!digest) return (
    <div>
      <BriefingTabs current="weekly" />
      <div className="card">暂无数据</div>
    </div>
  )

  return (
    <div>
      <BriefingTabs current="weekly" />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <h2 style={{ margin: 0 }}>本周精选 · {digest.week_start}</h2>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="secondary" title="查看上一周" onClick={() => setWeek(shiftWeek(digest.week_start, -7))}>‹ 上一周</button>
          <button className="secondary" title="查看下一周" onClick={() => setWeek(shiftWeek(digest.week_start, 7))}>下一周 ›</button>
          {week !== undefined && (
            <button className="secondary" title="回到本周" onClick={() => setWeek(undefined)}>本周</button>
          )}
        </div>
      </div>

      {digest.pending && digest.articles.length === 0 ? (
        <div className="card">周报生成中,稍后刷新…</div>
      ) : (
        <>
          {digest.intro_text ? (
            <div className="card" style={{ marginBottom: 16, lineHeight: 1.7 }}>
              {digest.intro_text}
            </div>
          ) : (
            <div className="card text-muted" style={{ marginBottom: 16, fontSize: 13 }}>
              {digest.articles.length === 0
                ? '本周暂无入选文章。'
                : '本周导语生成失败或暂未生成,以下是入选文章:'}
            </div>
          )}

          {digest.articles.length === 0 ? null : (
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
