import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Article, ArticleListItem } from '../api/client'
import './TweetCard.css'

interface Props {
  article: Article | ArticleListItem
  compact?: boolean
}

interface ParsedByline {
  handle: string
  displayName: string
  date: string
  body: string
}

// parseByline pulls the first-line byline produced by Go's
// rss.BuildTweetByline out of the article content. The expected shape is:
//   > @handle (DisplayName) · YYYY-MM-DD
// with DisplayName and the date each optional (BuildTweetByline degrades
// gracefully when either is missing). Returns the rest of the content
// as `body` with leading blank lines stripped so the markdown body
// starts cleanly. When the first line doesn't match (e.g. the article
// was somehow stored without a byline) we return empty author fields
// and pass the entire content through as the body.
function parseByline(content: string): ParsedByline {
  const lines = content.split('\n')
  const first = lines[0] || ''
  const m = first.match(/^>\s*@(\S+)(?:\s*\(([^)]+)\))?(?:\s*·\s*(.+?))?\s*$/)
  if (!m) return { handle: '', displayName: '', date: '', body: content }
  const body = lines.slice(1).join('\n').replace(/^\n+/, '')
  return {
    handle: m[1] || '',
    displayName: m[2] || '',
    date: m[3] || '',
    body,
  }
}

// TweetCard renders a tweet-shaped article. The byline is parsed out of
// `article.content` (rss.BuildTweetByline writes it as the first line
// when the worker stores tweets). Avatar uses unavatar.io as a free
// resolver — no API key needed, but it's an external dependency we
// don't control. In MVP this is the simplest path; a future iteration
// could store the avatar URL on the article model instead.
// articleContent reads `content` when the caller passed the full
// Article; ArticleListItem (the lean shape from the list endpoint)
// has no content field. In that case we fall back to summary_brief so
// the list still renders something useful — the user will see the
// full body when they open the detail page.
function articleContent(a: Article | ArticleListItem): string {
  if ('content' in a && typeof a.content === 'string') return a.content
  return a.summary_brief || ''
}

export default function TweetCard({ article, compact = false }: Props) {
  const { handle, displayName, date, body } = parseByline(articleContent(article))
  const avatarUrl = handle ? `https://unavatar.io/twitter/${encodeURIComponent(handle)}` : ''

  return (
    <div className={`tweet-card${compact ? ' tweet-card-compact' : ''}`}>
      <header className="tweet-card-header">
        {avatarUrl && <img src={avatarUrl} alt="" className="tweet-card-avatar" />}
        <div className="tweet-card-meta">
          {displayName && <span className="tweet-card-name">{displayName}</span>}
          {handle && <span className="tweet-card-handle">@{handle}</span>}
          {date && <span className="tweet-card-date"> · {date}</span>}
        </div>
      </header>
      <div className="tweet-card-body">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{body}</ReactMarkdown>
      </div>
      <footer className="tweet-card-footer">
        <a href={article.url} target="_blank" rel="noopener noreferrer">在 X 打开 ↗</a>
      </footer>
    </div>
  )
}
