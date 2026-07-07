interface Props {
  historicalPercent: number
  currentPercent: number
  aiMarkerPercent: number | null
}

export default function ArticleProgressBar({
  historicalPercent,
  currentPercent,
  aiMarkerPercent,
}: Props) {
  return (
    <div className="article-progress-track" aria-hidden="true">
      <div
        className="article-progress-fill article-progress-fill-history"
        style={{ width: `${historicalPercent}%` }}
      />
      <div
        className="article-progress-fill article-progress-fill-current"
        style={{ width: `${currentPercent}%` }}
      />
      {aiMarkerPercent !== null && (
        <div
          className="ai-marker"
          style={{ left: `${aiMarkerPercent}%` }}
          title="AI 总结结束"
          aria-label="AI summary end"
        >
          💡
        </div>
      )}
    </div>
  )
}
