import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { getDailyDigest, DailyDigest } from '../api/client'
import ReadingMeta from '../components/ReadingMeta'
import BriefingTabs from '../components/BriefingTabs'
import BriefingCalendar from '../components/BriefingCalendar'
import { writeNav } from '../utils/articleNav'
import { toast } from '../utils/toast'

export default function DailyPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()

  const [date, setDate] = useState<string | undefined>(() => params.get('date') ?? undefined)
  const [digest, setDigest] = useState<DailyDigest | null>(null)
  const [loading, setLoading] = useState(true)
  const [calOpen, setCalOpen] = useState(false)
  const calBtnRef = useRef<HTMLButtonElement>(null)
  const calPopRef = useRef<HTMLDivElement>(null)

  useEffect(() => { load(date) }, [date])

  useEffect(() => {
    if (date === undefined && params.get('date') !== null) {
      navigate('/daily', { replace: true })
    } else if (date !== undefined && params.get('date') !== date) {
      navigate('/daily?date=' + date, { replace: true })
    }
  }, [date])

  useEffect(() => {
    if (!calOpen) return
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (calPopRef.current?.contains(t)) return
      if (calBtnRef.current?.contains(t)) return
      setCalOpen(false)
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setCalOpen(false) }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [calOpen])

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

  const pickDate = (d: string) => {
    setDate(d)
    setCalOpen(false)
  }

  const onClickArticle = () => {
    if (!digest) return
    writeNav(digest.articles.map(a => a.id), null)
    try {
      sessionStorage.setItem('articleEntryPath', '/daily?date=' + digest.shown_date)
    } catch { /* ignore */ }
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

  const headerTitle = digest.mode === 'live'
    ? `今日精选 · ${digest.shown_date}（收集中）`
    : `本日精选 · ${digest.shown_date}`
  const showStaleTag = digest.pending && digest.shown_date !== digest.requested_date

  return (
    <div>
      <BriefingTabs current="daily" />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16, position: 'relative' }}>
        <h2 style={{ margin: 0 }}>{headerTitle}</h2>
        <button
          ref={calBtnRef}
          type="button"
          aria-label="选择日期"
          aria-expanded={calOpen}
          className="secondary"
          onClick={() => setCalOpen(o => !o)}
        >
          📅
        </button>
        {calOpen && (
          <div ref={calPopRef} style={{ position: 'absolute', top: '100%', right: 0, marginTop: 8, zIndex: 100 }}>
            <BriefingCalendar
              currentDate={digest.shown_date}
              onPick={pickDate}
              onClose={() => setCalOpen(false)}
            />
          </div>
        )}
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
                <Link
                  key={a.id}
                  to={`/articles/${a.id}`}
                  onClick={onClickArticle}
                  className="card"
                  style={{ display: 'block', textDecoration: 'none', color: 'inherit' }}
                >
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
