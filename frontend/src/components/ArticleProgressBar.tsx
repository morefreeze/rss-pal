interface Props {
  historicalPercent: number
}

export default function ArticleProgressBar({
  historicalPercent,
}: Props) {
  return (
    <div className="article-progress-track" aria-hidden="true">
      <div
        className="article-progress-fill article-progress-fill-history"
        style={{ width: `${historicalPercent}%` }}
      />
    </div>
  )
}
