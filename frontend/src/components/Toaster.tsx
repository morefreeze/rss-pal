import { useState, useEffect } from 'react'

interface ToastItem {
  id: number
  msg: string
  type: 'success' | 'error' | 'info'
}

let _id = 0

export default function Toaster() {
  const [toasts, setToasts] = useState<ToastItem[]>([])

  useEffect(() => {
    const handler = (e: Event) => {
      const { msg, type } = (e as CustomEvent).detail
      const id = ++_id
      setToasts(prev => [...prev, { id, msg, type }])
      setTimeout(() => {
        setToasts(prev => prev.filter(t => t.id !== id))
      }, type === 'error' ? 5000 : 3000)
    }
    window.addEventListener('show-toast', handler)
    return () => window.removeEventListener('show-toast', handler)
  }, [])

  if (toasts.length === 0) return null

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
          onClick={() => setToasts(prev => prev.filter(x => x.id !== t.id))}
          style={{
            padding: '10px 16px',
            borderRadius: 8,
            boxShadow: '0 4px 12px rgba(0,0,0,0.2)',
            fontSize: 14,
            color: 'white',
            cursor: 'pointer',
            backgroundColor:
              t.type === 'success' ? '#22c55e' :
              t.type === 'error' ? '#ef4444' :
              '#0066cc',
            animation: 'slideIn 0.2s ease',
          }}
        >
          {t.msg}
        </div>
      ))}
    </div>
  )
}
