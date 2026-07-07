interface Props {
  historicalPercent: number
  currentPercent: number
}

export default function ArticleProgressBar({
  historicalPercent,
  currentPercent,
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
    </div>
  )
}
