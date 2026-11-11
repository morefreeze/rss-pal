import { useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import AccountDisplay, { type DisplayAccount } from './AccountDisplay'

type SheetItem = { icon: string; label: string; to?: string; action?: 'logout' }

const ITEMS: SheetItem[] = [
  { icon: '🕘', label: '历史记录', to: '/history' },
  { icon: '📡', label: '订阅管理', to: '/feeds' },
  { icon: '💡', label: '兴趣',     to: '/interests' },
  { icon: '📊', label: '统计',     to: '/stats' },
  { icon: '⚙️', label: '设置',     to: '/settings' },
  { icon: '🚪', label: '登出',     action: 'logout' },
]

interface Props {
  user?: DisplayAccount | null
  isAdmin?: boolean
  open: boolean
  onClose: () => void
  onLogout: () => void
}

export default function MoreSheet({ open, onClose, onLogout, isAdmin = false, user }: Props) {
  const navigate = useNavigate()
  const dialog = useRef<HTMLDivElement>(null)
  const closeRef = useRef(onClose)
  closeRef.current = onClose

  useEffect(() => {
    if (!open) return
    const previous = document.activeElement as HTMLElement | null
    const focusables = () => Array.from(dialog.current?.querySelectorAll<HTMLElement>('button, a[href], [tabindex="0"]') || [])
    focusables()[0]?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.preventDefault(); closeRef.current() }
      if (e.key !== 'Tab') return
      const elements = focusables(), first = elements[0], last = elements[elements.length - 1]
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus() }
      if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus() }
    }
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    document.addEventListener('keydown', onKey)
    return () => { document.removeEventListener('keydown', onKey); document.body.style.overflow = previousOverflow; previous?.focus() }
  }, [open])

  if (!open) return null

  const items: SheetItem[] = isAdmin ? [{ icon: '🛡️', label: '后台管理', to: '/admin' }, ...ITEMS] : ITEMS

  const onItem = (item: SheetItem) => {
    onClose()
    if (item.action === 'logout') { onLogout(); return }
    if (item.to) navigate(item.to)
  }

  return (
    <>
      <div
        onClick={onClose}
        style={{
          position: 'fixed', inset: 0,
          background: 'rgba(0,0,0,0.35)',
          zIndex: 1200,
        }}
      />
      <div
        ref={dialog}
        role="dialog"
        aria-modal="true"
        aria-label="更多"
        style={{
          position: 'fixed',
          left: 0, right: 0, bottom: 0,
          maxHeight: '100dvh',
          overflowY: 'auto',
          background: 'var(--surface)',
          borderTop: '1px solid var(--border)',
          borderTopLeftRadius: 16,
          borderTopRightRadius: 16,
          padding: '8px 8px calc(env(safe-area-inset-bottom) + 16px) 8px',
          zIndex: 1201,
          boxShadow: '0 -4px 16px rgba(0,0,0,0.18)',
        }}
      >
        <div
          aria-hidden="true"
          style={{
            width: 36, height: 4, borderRadius: 2,
            background: 'var(--border)',
            margin: '8px auto 12px',
          }}
        />
        <button className="btn-ghost btn-sm" onClick={onClose} aria-label="关闭更多菜单" style={{ float: 'right' }}>✕</button>
        {user && <div style={{padding:'8px 16px 16px', borderBottom:'1px solid var(--border)'}}><AccountDisplay user={user} /></div>}
        {items.map(item => (
          <button
            key={item.label}
            type="button"
            onClick={() => onItem(item)}
            style={{
              display: 'flex', alignItems: 'center', gap: 12,
              width: '100%',
              padding: '14px 16px',
              height: 'auto',
              minHeight: 44,
              background: 'transparent',
              color: 'var(--fg)',
              border: 'none',
              borderRadius: 8,
              fontSize: 16,
              fontWeight: 500,
              textAlign: 'left',
            }}
          >
            <span style={{ fontSize: 20, width: 24, textAlign: 'center' }}>{item.icon}</span>
            <span>{item.label}</span>
          </button>
        ))}
      </div>
    </>
  )
}
