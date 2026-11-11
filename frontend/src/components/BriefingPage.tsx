import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { getDailyDigest, getWeeklyDigest, type DailyDigest, type WeeklyDigest, type BriefingTab } from '../api/client'
import { parsePublicationDate } from '../utils/publicationDate'
import { writeNav } from '../utils/articleNav'
import { weeklyEmptyStateMessage } from '../util/weeklyGenerationStatus'
import { useReadingWorkspace, WorkspaceSidebar, WorkspaceOverview } from '../workspace/ReadingWorkspaceContext'
import BriefingTabs from './BriefingTabs'
import BriefingCalendar from './BriefingCalendar'
import BriefingWCardStrip from './BriefingWCardStrip'
import ReadingMeta from './ReadingMeta'
import SummaryMarkdown from './SummaryMarkdown'

export default function BriefingPage({ kind }: { kind: BriefingTab }) {
  const workspace = useReadingWorkspace()
  const [params, setParams] = useSearchParams()
  const period = params.get(kind === 'daily' ? 'date' : 'week') || undefined
  const [digest, setDigest] = useState<DailyDigest | WeeklyDigest | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const [calendarOpen, setCalendarOpen] = useState(false)
  const calendarRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let active = true
    setLoading(true)
    setError('')
    setDigest(null)
    const request = kind === 'daily' ? getDailyDigest(period) : getWeeklyDigest(period)
    request.then(value => { if (active) setDigest(value) })
      .catch(() => { if (active) setError(kind === 'daily' ? '日报加载失败，请重试。' : '周报加载失败，请重试。') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [kind, period, retry])

  useEffect(() => {
    if (workspace && !workspace.isListVisible) { setCalendarOpen(false); return }
    if (!calendarOpen) return
    const onPointer = (event: MouseEvent) => {
      if (!calendarRef.current?.contains(event.target as Node)) setCalendarOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setCalendarOpen(false) }
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => { document.removeEventListener('mousedown', onPointer); document.removeEventListener('keydown', onKey) }
  }, [calendarOpen, workspace?.isListVisible])

  const daily = digest && 'shown_date' in digest ? digest : null
  const weekly = digest && 'week_start' in digest ? digest : null
  const shownPeriod = daily?.shown_date || weekly?.week_start || period || ''
  const issuePath = `/${kind}?${kind === 'daily' ? 'date' : 'week'}=${shownPeriod}`
  const title = kind === 'weekly' ? '本周精选' : daily?.mode === 'live' ? '今日精选' : '本日精选'
  const pickPeriod = (value: string) => {
    setParams({ [kind === 'daily' ? 'date' : 'week']: value })
    setCalendarOpen(false)
    workspace?.getListScrollElement()?.scrollTo(0, 0)
  }
  const saveEntry = () => {
    if (!digest) return
    writeNav(digest.articles.map(article => article.id), null)
    try { sessionStorage.setItem('articleEntryPath', issuePath) } catch { /* Browser storage can be unavailable. */ }
  }
  const periodControl = <div className="briefing-period-control" ref={calendarRef}>
    {daily && <>
      <button type="button" className="secondary" aria-label="选择日期" aria-expanded={calendarOpen} onClick={() => setCalendarOpen(open => !open)}>选择日期</button>
      {calendarOpen && <div className="briefing-calendar-popover"><BriefingCalendar currentDate={daily.shown_date} onPick={pickPeriod} onClose={() => setCalendarOpen(false)} /></div>}
    </>}
    {weekly && <BriefingWCardStrip currentWeekStart={weekly.week_start} onPick={pickPeriod} />}
  </div>
  const emptyMessage = weekly && weekly.articles.length === 0 ? weeklyEmptyStateMessage(weekly)
    : daily?.pending && daily.articles.length === 0 ? '日报生成中，稍后刷新。' : '当期暂无入选文章。'

  return <div className="briefing-page">
    {workspace && <WorkspaceSidebar>
      <h3 className="saved-section-title">简报归档</h3>
      <BriefingTabs current={kind} />
      <p className="text-muted text-sm">{shownPeriod}</p>
      {!workspace.isPhone && periodControl}
    </WorkspaceSidebar>}
    <div className="briefing-list-heading">
      {workspace ? <><h2>简报</h2><button type="button" className="btn-ghost workspace-scope-button" onClick={workspace.openScopes}>日报与周报</button></> : <BriefingTabs current={kind} />}
      {(!workspace || workspace.isPhone) && periodControl}
      <p className="text-muted text-sm">{shownPeriod}{kind === 'weekly' ? ' 当周' : ''}</p>
    </div>
    {loading ? <div className="card" role="status">加载中…</div> : error ? <div className="card" role="alert">{error} <button onClick={() => setRetry(value => value + 1)}>重试</button></div> : digest && <>
      <WorkspaceOverview>
        <div className="briefing-overview">
          <div className="text-muted text-sm">{shownPeriod} · {kind === 'daily' ? '日报' : '周报'}</div>
          <h1>{title}</h1>
          {daily?.pending && daily.shown_date !== daily.requested_date && <p role="status">{daily.requested_date} 的日报还在生成中，当前展示 {daily.shown_date} 的内容。</p>}
          {daily?.mode === 'live' && <p className="text-muted">今日还在收集中，明早 5 点后生成正式日报。</p>}
          {digest.intro_text ? <div className="briefing-intro"><SummaryMarkdown source={digest.intro_text} /></div>
            : digest.articles.length > 0 && <p className="text-muted">导语暂未生成，可以先阅读本期入选文章。</p>}
          {digest.articles.length === 0 ? <p>{emptyMessage}</p> : <p className="text-muted text-sm">本期入选 {digest.articles.length} 篇文章</p>}
        </div>
      </WorkspaceOverview>
      <div className="briefing-article-list">
        {digest.articles.map(article => {
          const date = parsePublicationDate(article.published_at)
          return <Link key={article.id} to={`/articles/${article.id}`} state={{ from: issuePath, articlePreview: article }} onClick={saveEntry}
            className="card briefing-article-row" data-article-card data-article-id={article.id}
            aria-current={workspace?.selectedArticleId === article.id ? 'page' : undefined}
            data-selected={workspace?.selectedArticleId === article.id || undefined}>
            <div className="article-card-title">{article.title}</div>
            <div className="briefing-article-meta text-muted text-sm">
              <span>{article.source?.title || article.feed_title}</span>
              {date && <span>{date.toLocaleDateString('zh-CN')}</span>}
              <ReadingMeta wordCount={article.word_count} readingMinutes={article.reading_minutes} mediaType={article.media_type} mediaDurationSeconds={article.media_duration_seconds} />
            </div>
            {article.summary_brief && <div className="article-card-summary text-muted text-sm"><SummaryMarkdown source={article.summary_brief} /></div>}
          </Link>
        })}
        {digest.articles.length === 0 && workspace && !workspace.isPhone && <p className="text-muted">{emptyMessage}</p>}
      </div>
    </>}
  </div>
}
