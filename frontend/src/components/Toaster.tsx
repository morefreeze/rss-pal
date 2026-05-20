import { useState, useEffect, useRef } from 'react'

interface ToastItem {
  id: number
  msg: string
  type: 'success' | 'error' | 'info'
  durationMs: number
  action?: { label: string; onClick: () => void }
  // Bumped each time the dismissal timer is (re)armed — i.e. on mount and
  // on every mouse-leave. The progress bar uses this as a React key so it
  // remounts and the CSS animation restarts from 100% width.
  runKey: number
  hovered: boolean
}

let _id = 0

export default function Toaster() {
  const [toasts, setToasts] = useState<ToastItem[]>([])
  // One pending dismissal timer per toast id. Hover clears it, mouse-leave
  // re-arms it with the full duration ("重新计时"), matching the user's spec.
  const timers = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map())

  const clearTimer = (id: number) => {
    const t = timers.current.get(id)
    if (t) {
      clearTimeout(t)
      timers.current.delete(id)
    }
  }

  const scheduleDismiss = (id: number, ms: number) => {
    clearTimer(id)
    timers.current.set(id, setTimeout(() => {
      timers.current.delete(id)
      setToasts(prev => prev.filter(x => x.id !== id))
    }, ms))
  }

  useEffect(() => {
    const handler = (e: Event) => {
      const { msg, type, action, durationMs } = (e as CustomEvent).detail
      const id = ++_id
      const defaultMs = action ? 5000 : (type === 'error' ? 5000 : 3000)
      const ms = durationMs ?? defaultMs
      setToasts(prev => [...prev, { id, msg, type, durationMs: ms, action, runKey: 1, hovered: false }])
      scheduleDismiss(id, ms)
    }
    window.addEventListener('show-toast', handler)
    return () => {
      window.removeEventListener('show-toast', handler)
      timers.current.forEach(t => clearTimeout(t))
      timers.current.clear()
    }
  }, [])

  if (toasts.length === 0) return null

  const dismiss = (id: number) => {
    clearTimer(id)
    setToasts(prev => prev.filter(x => x.id !== id))
  }

  const onEnter = (id: number) => {
    clearTimer(id)
    setToasts(prev => prev.map(t => t.id === id ? { ...t, hovered: true } : t))
  }
  const onLeave = (t: ToastItem) => {
    setToasts(prev => prev.map(x => x.id === t.id ? { ...x, hovered: false, runKey: x.runKey + 1 } : x))
    scheduleDismiss(t.id, t.durationMs)
  }

  return (
    <div style={{
      position: 'fixed',
      top: 16,
      left: '50%',
      transform: 'translateX(-50%)',
      zIndex: 9999,
      display: 'flex',
      flexDirection: 'column',
      gap: 8,
      maxWidth: 'min(420px, 90vw)',
      width: 'max-content',
      pointerEvents: 'none',
    }}>
      {toasts.map(t => {
        const bg =
          t.type === 'success' ? '#22c55e' :
          t.type === 'error' ? '#ef4444' :
          '#0066cc'
        return (
          <div
            key={t.id}
            onClick={t.action ? undefined : () => dismiss(t.id)}
            onMouseEnter={() => onEnter(t.id)}
            onMouseLeave={() => onLeave(t)}
            style={{
              pointerEvents: 'auto',
              position: 'relative',
              padding: '10px 16px',
              borderRadius: 8,
              boxShadow: '0 4px 12px rgba(0,0,0,0.2)',
              fontSize: 14,
              color: 'white',
              cursor: t.action ? 'default' : 'pointer',
              backgroundColor: bg,
              animation: 'slideIn 0.2s ease',
              display: 'flex',
              alignItems: 'center',
              gap: 12,
              justifyContent: 'space-between',
              overflow: 'hidden',
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
            {/* Countdown bar — anchored right edge so the visual shrinkage
                reads right-to-left. Hover snaps it back to full and pauses;
                mouse-leave remounts (via runKey) so the animation starts
                fresh at 100%. */}
            <div
              key={t.runKey}
              style={{
                position: 'absolute',
                left: 0,
                right: 0,
                bottom: 0,
                height: 3,
                background: 'rgba(255,255,255,0.5)',
                transformOrigin: 'right center',
                transform: 'scaleX(1)',
                animation: t.hovered ? 'none' : `toast-progress ${t.durationMs}ms linear forwards`,
              }}
            />
          </div>
        )
      })}
    </div>
  )
}
