import { Link } from 'react-router-dom'
import type { Article } from '../api/client'
import SummaryMarkdown from './SummaryMarkdown'

interface Props {
  children: Article[]
  onRetry: (task: Article) => void
}

function hostOf(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

export function LinkSetChildren({ children, onRetry }: Props) {
  const items = [...children].sort((a, b) => a.id - b.id)

  if (items.length === 0) {
    return (
      <section
        className="mt-8 p-4 rounded text-sm"
        style={{ border: '1px solid var(--border)', color: 'var(--fg-muted)' }}
      >
        本期没有提取到链接。
      </section>
    )
  }

  return (
    <section className="mt-8 pt-6" style={{ borderTop: '1px solid var(--border)' }}>
      <h2 className="text-lg font-semibold mb-4">已抓取的链接（{items.length} 条）</h2>

      <div className="space-y-3">
        {items.map((c) => (
          <article
            key={c.id}
            className="p-4 rounded-md"
            style={{ border: '1px solid var(--border)', background: 'var(--bg-elevated)' }}
          >
            <Link
              to={`/articles/${c.id}`}
              className="font-medium hover:underline"
              style={{ color: 'var(--fg)' }}
            >
              {c.title}
            </Link>
            <div className="text-xs mt-1" style={{ color: 'var(--fg-muted)' }}>
              {hostOf(c.url)}
            </div>
            {c.editor_note && (
              <p className="text-sm mt-2 italic" style={{ color: 'var(--fg-muted)' }}>
                {c.editor_note}
              </p>
            )}
            {c.processing_state === 'processing' && (
              <p className="text-xs mt-2" style={{ color: 'var(--fg-muted)' }}>
                正在抓取…
              </p>
            )}
            {c.processing_state === 'failed' && (
              <button
                className="mt-2 text-xs px-2 py-1 rounded"
                style={{ border: '1px solid var(--border)', color: 'var(--fg)' }}
                onClick={() => onRetry(c)}
              >
                重试
              </button>
            )}
            {c.processing_state === 'ready' && c.summary_brief && (
              <div className="summary-markdown text-sm mt-2" style={{ color: 'var(--fg)' }}>
                <SummaryMarkdown source={c.summary_brief} />
              </div>
            )}
          </article>
        ))}
      </div>
    </section>
  )
}
