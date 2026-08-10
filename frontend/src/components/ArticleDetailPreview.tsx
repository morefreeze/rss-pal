import type { ArticleListItem } from '../api/client'
import SummaryMarkdown from './SummaryMarkdown'

export default function ArticleDetailPreview({
  article,
}: {
  article: ArticleListItem
}) {
  const metadata = [
    article.feed_title,
    article.published_at
      ? new Date(article.published_at).toLocaleString('zh-CN')
      : '',
  ].filter(Boolean).join(' · ')

  return (
    <div className="card" aria-live="polite">
      <h2 style={{ marginTop: 0 }}>{article.title}</h2>
      {metadata && <div className="text-muted text-sm">{metadata}</div>}
      {article.summary_brief && (
        <div className="summary-markdown text-muted">
          <SummaryMarkdown source={article.summary_brief} />
        </div>
      )}
      <div className="text-muted text-sm">正在加载正文…</div>
    </div>
  )
}
