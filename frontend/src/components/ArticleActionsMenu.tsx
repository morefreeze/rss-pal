import { useEffect, useRef, useState } from 'react'

interface Props {
  // Only action right now; kept as an array so future low-frequency actions
  // (举报, 复制 ID, …) can slide in without rewiring callers.
  onDelete: () => void
}

export default function ArticleActionsMenu({ onDelete }: Props) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onClick = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={rootRef} style={{ position: 'relative', display: 'inline-block' }}>
      <button
        type="button"
        className="secondary"
        onClick={() => setOpen(v => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        title="更多操作"
        style={{ fontSize: 13, padding: '4px 10px' }}
      >
        ⋯
      </button>
      {open && (
        <div
          role="menu"
          style={{
            position: 'absolute',
            top: 'calc(100% + 4px)',
            right: 0,
            minWidth: 140,
            background: 'var(--bg-elevated, white)',
            border: '1px solid var(--border, #e5e5e5)',
            borderRadius: 6,
            boxShadow: '0 6px 20px rgba(0,0,0,0.12)',
            zIndex: 100,
            padding: 4,
          }}
        >
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              setOpen(false)
              onDelete()
            }}
            style={{
              display: 'block',
              width: '100%',
              textAlign: 'left',
              padding: '8px 12px',
              border: 'none',
              background: 'transparent',
              color: '#dc2626',
              fontSize: 14,
              cursor: 'pointer',
              borderRadius: 4,
            }}
            onMouseEnter={e => (e.currentTarget.style.background = 'rgba(220,38,38,0.08)')}
            onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
          >
            🗑 删除
          </button>
        </div>
      )}
    </div>
  )
}
