import { useState, useEffect } from 'react'

interface ToastItem {
  id: number
  msg: string
  type: 'success' | 'error' | 'info'
  action?: { label: string; onClick: () => void }
}

let _id = 0

export default function Toaster() {
  const [toasts, setToasts] = useState<ToastItem[]>([])

  useEffect(() => {
    const handler = (e: Event) => {
      const { msg, type, action, durationMs } = (e as CustomEvent).detail
      const id = ++_id
      setToasts(prev => [...prev, { id, msg, type, action }])
      const defaultMs = action ? 5000 : (type === 'error' ? 5000 : 3000)
      setTimeout(() => {
        setToasts(prev => prev.filter(t => t.id !== id))
      }, durationMs ?? defaultMs)
    }
    window.addEventListener('show-toast', handler)
    return () => window.removeEventListener('show-toast', handler)
  }, [])

  if (toasts.length === 0) return null

  const dismiss = (id: number) =>
    setToasts(prev => prev.filter(x => x.id !== id))

  return (
    <div style={{
      position: 'fixed',
      bottom: 20,
      right: 20,
      zIndex: 9999,
      display: 'flex',
      flexDirection: 'column',
      gap: 8,
      maxWidth: 360,
    }}>
      {toasts.map(t => (
        <div
          key={t.id}
          onClick={t.action ? undefined : () => dismiss(t.id)}
          style={{
            padding: '10px 16px',
            borderRadius: 8,
            boxShadow: '0 4px 12px rgba(0,0,0,0.2)',
            fontSize: 14,
            color: 'white',
            cursor: t.action ? 'default' : 'pointer',
            backgroundColor:
              t.type === 'success' ? '#22c55e' :
              t.type === 'error' ? '#ef4444' :
              '#0066cc',
            animation: 'slideIn 0.2s ease',
            display: 'flex',
            alignItems: 'center',
            gap: 12,
            justifyContent: 'space-between',
          }}
        >
          <span>{t.msg}</span>
          {t.action && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                t.action!.onClick()
                dismiss(t.id)
              }}
              style={{
                background: 'rgba(255,255,255,0.2)',
                color: 'white',
                border: '1px solid rgba(255,255,255,0.4)',
                borderRadius: 4,
                padding: '2px 10px',
                fontSize: 13,
                cursor: 'pointer',
                flexShrink: 0,
              }}
            >
              {t.action.label}
            </button>
          )}
        </div>
      ))}
    </div>
  )
}
