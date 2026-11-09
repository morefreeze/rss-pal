import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getReadingHistory, type ReadingHistoryPageData } from '../api/client'
import './ReadingHistoryPage.css'

const kindLabels = { article: '订阅文章', clip: '网摘', explore: '探索' }
const openedAtFormat = new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' })

export default function ReadingHistoryPage() {
  const [page, setPage] = useState<ReadingHistoryPageData | null>(null)
  const [request, setRequest] = useState<{ before?: string }>({})
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    getReadingHistory(request.before, controller.signal)
      .then(next => {
        if (controller.signal.aborted) return
        setPage(current => request.before && current
          ? { ...next, items: [...current.items, ...next.items] }
          : next)
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [request])

  const load = (before?: string) => {
    setFailed(false)
    setLoading(true)
    setRequest({ before })
  }

  return (
    <section className="reading-history" aria-labelledby="reading-history-title">
      <h2 id="reading-history-title">历史记录</h2>
      <div className="reading-history-panel" aria-busy={loading}>
        {!!page?.items.length && (
          <ul className="reading-history-list">
            {page.items.map(item => {
              const available = item.available && item.article_id !== null
              const source = kindLabels[item.kind]
              const details = (
                <>
                  <strong>{item.title || '无标题'}</strong>
                  <span className="reading-history-meta">
                    <span>
                      {source}{item.source_title && item.source_title !== source ? ` · ${item.source_title}` : ''}
                      {!available && ' · 文章暂不可打开'}
                    </span>
                    <time dateTime={item.opened_at}>{openedAtFormat.format(new Date(item.opened_at))}</time>
                  </span>
                </>
              )
              return (
                <li key={item.id}>
                  {available ? (
                    <Link
                      className="reading-history-row"
                      to={`${item.kind === 'explore' ? '/explore' : ''}/articles/${item.article_id}`}
                      state={{ from: '/history' }}
                    >
                      {details}
                    </Link>
                  ) : <div className="reading-history-row">{details}</div>}
                </li>
              )
            })}
          </ul>
        )}
        {!loading && !failed && page?.items.length === 0 && (
          <p className="reading-history-status text-muted">打开过的文章会显示在这里。</p>
        )}
        {loading && <p className="reading-history-status text-muted" role="status">正在加载历史记录…</p>}
        {failed && (
          <div className="reading-history-status" role="alert">
            <p>历史记录加载失败。</p>
            <button type="button" className="secondary" onClick={() => load(request.before)}>重试</button>
          </div>
        )}
        {!loading && !failed && page?.next_cursor && (
          <div className="reading-history-status">
            <button type="button" className="secondary" onClick={() => load(page.next_cursor)}>加载更多</button>
          </div>
        )}
      </div>
    </section>
  )
}
