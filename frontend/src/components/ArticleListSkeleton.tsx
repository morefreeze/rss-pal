import './Skeleton.css'

interface Props {
  rows?: number
}

// ArticleListSkeleton renders placeholder rows that mimic the layout
// of ArticleRow (title bar + brief summary + metadata strip). Used
// on the first article-list paint so the user sees structure rather
// than a blank page on weak networks.
export function ArticleListSkeleton({ rows = 8 }: Props) {
  return (
    <div role="status" aria-label="Loading articles" aria-live="polite">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="skeleton-row">
          <div className="skeleton-pulse skeleton-bar-tall" style={{ width: `${60 + (i % 4) * 8}%` }} />
          <div style={{ height: 6 }} />
          <div className="skeleton-pulse skeleton-bar" style={{ width: '92%' }} />
          <div style={{ height: 6 }} />
          <div className="skeleton-pulse skeleton-bar" style={{ width: '40%' }} />
        </div>
      ))}
    </div>
  )
}
