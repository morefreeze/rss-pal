// Small trailing icon rendered next to candidate <a> tags inside article
// markdown. Click toggles the "mark for batch fetch" state. Stops both
// default and propagation so the surrounding link doesn't navigate.
//
// Mailbox metaphor:
//   📪  unmarked  — closed mailbox, flag down ("nothing queued")
//   📫  marked    — closed mailbox, flag up   ("queued for pickup")
//   📬  fetched   — open mailbox, mail in     ("delivered, not actionable")

import type { MouseEvent } from 'react'

type Props = {
  marked: boolean
  alreadyFetched: boolean
  onToggle: () => void
}

export function LinkSetMarkIcon({ marked, alreadyFetched, onToggle }: Props) {
  const handleClick = (e: MouseEvent<HTMLButtonElement>) => {
    e.preventDefault()
    e.stopPropagation()
    if (alreadyFetched) return
    onToggle()
  }

  const title = alreadyFetched
    ? '已抓取'
    : marked
      ? '取消标记（不会抓取）'
      : '标记为批量抓取'

  const icon = alreadyFetched ? '📬' : marked ? '📫' : '📪'

  return (
    <button
      type="button"
      onClick={handleClick}
      title={title}
      aria-label={title}
      aria-pressed={marked}
      disabled={alreadyFetched}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        verticalAlign: 'baseline',
        marginLeft: 4,
        padding: 0,
        width: 18,
        height: 18,
        border: 'none',
        background: 'transparent',
        cursor: alreadyFetched ? 'not-allowed' : 'pointer',
        fontSize: 14,
        lineHeight: 1,
      }}
    >
      {icon}
    </button>
  )
}
