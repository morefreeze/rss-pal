import { useCallback, useEffect, useRef, useState } from 'react'

type Props = {
  articleId: number
  icon: string
  label: string
  variant: 'primary' | 'outline'
  onActivate: () => void | Promise<void>
  title?: string
}

const KEY_PREFIX = 'rsspal:fab-collapsed:'
const DRAG_THRESHOLD_PX = 6
const COLLAPSE_RATIO = 0.5
const TRANSITION_MS = 150

function loadCollapsed(articleId: number): boolean {
  if (!articleId) return false
  try {
    return localStorage.getItem(KEY_PREFIX + articleId) === '1'
  } catch {
    return false
  }
}

function saveCollapsed(articleId: number, collapsed: boolean) {
  if (!articleId) return
  try {
    if (collapsed) localStorage.setItem(KEY_PREFIX + articleId, '1')
    else localStorage.removeItem(KEY_PREFIX + articleId)
  } catch {
    // ignore
  }
}

// Sweeps every rsspal:fab-collapsed:* entry. Exported so logout() in
// api/client can clear FAB state alongside other per-session reader state.
export function clearAllFabCollapsed() {
  try {
    const keys: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i)
      if (k && k.startsWith(KEY_PREFIX)) keys.push(k)
    }
    keys.forEach(k => localStorage.removeItem(k))
  } catch {
    // ignore
  }
}

export default function CollapsibleFab({
  articleId,
  icon,
  label,
  variant,
  onActivate,
  title,
}: Props) {
  const [collapsed, setCollapsed] = useState(() => loadCollapsed(articleId))
  const [dragX, setDragX] = useState(0)
  const dragStartRef = useRef<number | null>(null)
  const draggedRef = useRef(false)
  const pillWidthRef = useRef<number>(0)
  const pillRef = useRef<HTMLButtonElement>(null)

  // Re-read persisted state when the article changes (FAB instance reused
  // across navigation via React).
  useEffect(() => {
    setCollapsed(loadCollapsed(articleId))
    setDragX(0)
  }, [articleId])

  const collapse = useCallback(() => {
    setCollapsed(true)
    setDragX(0)
    saveCollapsed(articleId, true)
  }, [articleId])

  const expand = useCallback(() => {
    setCollapsed(false)
    setDragX(0)
    saveCollapsed(articleId, false)
  }, [articleId])

  const onTouchStart = (e: React.TouchEvent) => {
    const t = e.touches[0]
    if (!t) return
    dragStartRef.current = t.clientX
    draggedRef.current = false
    pillWidthRef.current = pillRef.current?.offsetWidth ?? 0
  }

  const onTouchMove = (e: React.TouchEvent) => {
    if (dragStartRef.current === null) return
    const t = e.touches[0]
    if (!t) return
    const dx = t.clientX - dragStartRef.current
    if (Math.abs(dx) > DRAG_THRESHOLD_PX) draggedRef.current = true
    // While expanded, only positive drag (right) is meaningful; clamp.
    // While collapsed, only negative drag (left) is meaningful; clamp.
    const clamped = collapsed ? Math.min(0, dx) : Math.max(0, dx)
    setDragX(clamped)
  }

  const onTouchEnd = () => {
    const dx = dragX
    const width = pillWidthRef.current || 100
    if (collapsed) {
      // Drag left far enough → expand. Otherwise snap back to collapsed.
      if (-dx > width * COLLAPSE_RATIO) expand()
      else setDragX(0)
    } else {
      // Drag right far enough → collapse. Otherwise snap back to expanded.
      if (dx > width * COLLAPSE_RATIO) collapse()
      else setDragX(0)
    }
    dragStartRef.current = null
    // Defer clearing the dragged flag so the click handler can see it.
    setTimeout(() => { draggedRef.current = false }, 0)
  }

  const handlePillClick = () => {
    if (draggedRef.current) return
    onActivate()
  }

  const baseStyles: React.CSSProperties = {
    position: 'fixed',
    right: 24,
    bottom: 152,
    zIndex: 1100,
    transition: `transform ${TRANSITION_MS}ms ease, opacity ${TRANSITION_MS}ms ease`,
  }

  if (collapsed) {
    // Tab/strip flush to the right edge. Drag offset (dragX is negative
    // while dragging left) eases it inward as the user pulls.
    return (
      <button
        type="button"
        onClick={() => { if (!draggedRef.current) expand() }}
        onTouchStart={onTouchStart}
        onTouchMove={onTouchMove}
        onTouchEnd={onTouchEnd}
        title={`展开 ${label}`}
        aria-label={`展开 ${label}`}
        style={{
          ...baseStyles,
          right: 0,
          width: 18,
          height: 56,
          padding: 0,
          borderTopLeftRadius: 10,
          borderBottomLeftRadius: 10,
          borderTopRightRadius: 0,
          borderBottomRightRadius: 0,
          border: variant === 'primary' ? 'none' : '1px solid var(--accent)',
          background: variant === 'primary' ? 'var(--accent)' : 'var(--bg-elevated, #fff)',
          color: variant === 'primary' ? 'var(--accent-fg)' : 'var(--accent)',
          fontSize: 14,
          lineHeight: 1,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          boxShadow: '-2px 4px 12px rgba(0,0,0,0.18)',
          transform: `translateX(${dragX}px)`,
          cursor: 'pointer',
          touchAction: 'pan-y',
        }}
      >
        <span aria-hidden style={{ fontSize: 14 }}>{icon}</span>
      </button>
    )
  }

  return (
    <button
      ref={pillRef}
      type="button"
      onClick={handlePillClick}
      onTouchStart={onTouchStart}
      onTouchMove={onTouchMove}
      onTouchEnd={onTouchEnd}
      title={title ?? label}
      style={{
        ...baseStyles,
        padding: '10px 8px 10px 16px',
        borderRadius: 24,
        border: variant === 'primary' ? 'none' : '1px solid var(--accent)',
        background: variant === 'primary' ? 'var(--accent)' : 'var(--bg-elevated, #fff)',
        color: variant === 'primary' ? 'var(--accent-fg)' : 'var(--accent)',
        cursor: 'pointer',
        boxShadow: variant === 'primary' ? '0 4px 12px rgba(0,0,0,0.18)' : '0 4px 12px rgba(0,0,0,0.12)',
        fontSize: 13,
        fontWeight: 500,
        whiteSpace: 'nowrap',
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        transform: `translateX(${dragX}px)`,
        touchAction: 'pan-y',
      }}
    >
      <span style={{ whiteSpace: 'nowrap' }}>{icon} {label}</span>
      <span
        role="button"
        tabIndex={0}
        aria-label={`收起 ${label}`}
        onClick={(e) => { e.stopPropagation(); collapse() }}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            e.stopPropagation()
            collapse()
          }
        }}
        style={{
          marginLeft: 4,
          width: 22,
          height: 22,
          borderRadius: 11,
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: 14,
          lineHeight: 1,
          cursor: 'pointer',
          background: variant === 'primary' ? 'rgba(255,255,255,0.18)' : 'rgba(0,0,0,0.05)',
          color: 'inherit',
        }}
      >
        ✕
      </span>
    </button>
  )
}
