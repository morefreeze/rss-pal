import './Skeleton.css'

// ArticleDetailSkeleton renders a title bar and a few paragraph bars,
// shown while the detail endpoint is in flight on first navigation.
export function ArticleDetailSkeleton() {
  return (
    <div role="status" aria-label="Loading article" aria-live="polite" style={{ padding: '16px 0' }}>
      <div className="skeleton-pulse skeleton-bar-tall" style={{ width: '70%', height: 28 }} />
      <div style={{ height: 12 }} />
      <div className="skeleton-pulse skeleton-bar" style={{ width: '30%' }} />
      <div style={{ height: 24 }} />
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i}>
          <div className="skeleton-pulse skeleton-bar" style={{ width: i === 4 ? '60%' : '100%' }} />
          <div style={{ height: 10 }} />
        </div>
      ))}
    </div>
  )
}
