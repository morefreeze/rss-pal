import { useEffect, useRef, useState, type ReactNode } from 'react'

interface Props {
  // Content rendered inside the popover. Stacked vertically by default — pass
  // a wrapping fragment to render multiple controls.
  children: ReactNode
  title?: string
}

// Generic kebab overflow: a ⋯ button that opens a small popover containing
// whatever was passed in. Used on the article list toolbar to tuck away the
// extra controls (search, feed select, sort, 分组) on narrow viewports.
export default function OverflowMenu({ children, title = '更多' }: Props) {
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
        className="btn-ghost"
        onClick={() => setOpen(v => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        title={title}
        style={{ padding: '4px 10px', minWidth: 0 }}
      >
        ⋯
      </button>
      {open && (
        <div
          role="menu"
          style={{
            position: 'absolute',
            top: 'calc(100% + 6px)',
            right: 0,
            minWidth: 240,
            maxWidth: 'min(320px, 90vw)',
            background: 'var(--bg-elevated, var(--surface, white))',
            border: '1px solid var(--border, #e5e5e5)',
            borderRadius: 8,
            boxShadow: '0 6px 20px rgba(0,0,0,0.12)',
            zIndex: 100,
            padding: 10,
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
          }}
        >
          {children}
        </div>
      )}
    </div>
  )
}
