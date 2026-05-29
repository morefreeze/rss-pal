import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'

type SheetItem = { icon: string; label: string; to?: string; action?: 'logout' }

const ITEMS: SheetItem[] = [
  { icon: '✨', label: '推荐',     to: '/recommended' },
  { icon: '💡', label: '洞察',     to: '/insights' },
  { icon: '📊', label: '统计',     to: '/stats' },
  { icon: '⚙️', label: '设置',     to: '/settings' },
  { icon: '🚪', label: '登出',     action: 'logout' },
]

interface Props {
  open: boolean
  onClose: () => void
  onLogout: () => void
}

export default function MoreSheet({ open, onClose, onLogout }: Props) {
  const navigate = useNavigate()

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

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
        role="dialog"
        aria-label="更多"
        style={{
          position: 'fixed',
          left: 0, right: 0, bottom: 0,
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
        {ITEMS.map(item => (
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
