interface Props {
  wordCount?: number
  readingMinutes?: number
  mediaType?: string
  mediaDurationSeconds?: number
  className?: string
}

export function formatVideoDuration(seconds?: number): string {
  if (!seconds || seconds <= 0 || !Number.isFinite(seconds)) return '视频 · 时长未知'
  const total = Math.floor(seconds)
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor(total / 60) % 60
  const secs = String(total % 60).padStart(2, '0')
  return `视频 ${hours ? `${hours}:${String(minutes).padStart(2, '0')}` : minutes}:${secs}`
}

export default function ReadingMeta({ wordCount, readingMinutes, mediaType, mediaDurationSeconds, className }: Props) {
  const hasText = Boolean(wordCount && wordCount > 0)
  const video = mediaType?.startsWith('video/') || mediaType === 'youtube' || mediaType === 'bilibili'
  if (!hasText && !video) return null
  return (
    <span className={className} style={{ color: 'var(--fg-muted)', fontSize: 12 }}>
      {hasText && <span>📖 {wordCount!.toLocaleString('zh-CN')} 字 · {readingMinutes && readingMinutes > 0 ? readingMinutes : 1} 分钟</span>}
      {hasText && video && ' · '}
      {video && <span>{formatVideoDuration(mediaDurationSeconds)}</span>}
    </span>
  )
}
