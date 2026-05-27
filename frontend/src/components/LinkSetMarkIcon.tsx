// Small trailing icon rendered next to candidate <a> tags inside article
// markdown. Click toggles the "mark for batch fetch" state. Stops both
// default and propagation so the surrounding link doesn't navigate.

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

  // SVG icons inline so we don't need a sprite or icon library.
  // ✓-in-circle for fetched, filled download arrow for marked, faded for unmarked.
  const icon = alreadyFetched ? (
    <svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true">
      <circle cx="8" cy="8" r="7" fill="currentColor" opacity="0.2" />
      <path
        d="M4.5 8.5 L7 11 L11.5 5.5"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
    </svg>
  ) : marked ? (
    <svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true">
      <path
        d="M8 2 V11 M4 7 L8 11 L12 7 M3 13.5 H13"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
    </svg>
  ) : (
    <svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true">
      <path
        d="M8 2 V11 M4 7 L8 11 L12 7 M3 13.5 H13"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
        opacity="0.55"
      />
    </svg>
  )

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
        padding: 1,
        width: 16,
        height: 16,
        border: 'none',
        background: 'transparent',
        cursor: alreadyFetched ? 'not-allowed' : 'pointer',
        color: alreadyFetched
          ? 'var(--fg-muted)'
          : marked
            ? 'var(--accent, #2563eb)'
            : 'var(--fg-muted)',
        opacity: alreadyFetched ? 0.5 : marked ? 1 : 0.6,
        transition: 'opacity 120ms, color 120ms',
        lineHeight: 1,
      }}
      onMouseEnter={(e) => { if (!alreadyFetched && !marked) e.currentTarget.style.opacity = '1' }}
      onMouseLeave={(e) => { if (!alreadyFetched && !marked) e.currentTarget.style.opacity = '0.6' }}
    >
      {icon}
    </button>
  )
}
