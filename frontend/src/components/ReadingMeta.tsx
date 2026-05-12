interface Props {
  wordCount?: number
  readingMinutes?: number
  className?: string
}

// Renders "📖 1,234 字 · 5 分钟" — silent if word_count is missing or 0.
export default function ReadingMeta({ wordCount, readingMinutes, className }: Props) {
  if (!wordCount || wordCount <= 0) return null
  const formatted = wordCount.toLocaleString('zh-CN')
  const mins = readingMinutes && readingMinutes > 0 ? readingMinutes : 1
  return (
    <span className={className} style={{ color: 'var(--fg-muted)', fontSize: 12 }}>
      📖 {formatted} 字 · {mins} 分钟
    </span>
  )
}
