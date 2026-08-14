import { MouseEvent, CSSProperties } from 'react'
import { buildFeedFilterPath, rememberFeedFilter } from '../utils/feedFilterLink'

interface Props {
  feedId: number
  label: string
  search?: string
  className?: string
  style?: CSSProperties
  onNavigate?: (feedId: number, href: string) => void
}

export default function FeedSourceLink({
  feedId,
  label,
  search = '',
  className = 'tag-chip tag-chip-source',
  style,
  onNavigate,
}: Props) {
  const href = buildFeedFilterPath(feedId, search)

  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    event.stopPropagation()
    rememberFeedFilter(feedId)
    if (onNavigate) {
      event.preventDefault()
      onNavigate(feedId, href)
    }
  }

  return (
    <a
      href={href}
      className={className}
      style={{ textDecoration: 'none', ...style }}
      aria-label={`查看 ${label} 的文章`}
      onClick={handleClick}
    >
      <span>{label}</span>
    </a>
  )
}
