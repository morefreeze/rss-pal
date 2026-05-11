import { useEffect, useState } from 'react'

interface BackFabProps {
  onClick: () => void
}

export default function BackFab({ onClick }: BackFabProps) {
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    let raf = 0
    const onScroll = () => {
      if (raf) return
      raf = requestAnimationFrame(() => {
        setVisible(window.scrollY > window.innerHeight)
        raf = 0
      })
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    onScroll()
    return () => {
      window.removeEventListener('scroll', onScroll)
      if (raf) cancelAnimationFrame(raf)
    }
  }, [])

  return (
    <button
      type="button"
      aria-label="返回"
      title="返回 (Esc)"
      onClick={onClick}
      style={{
        position: 'fixed',
        left: 24,
        bottom: 88,
        width: 48,
        height: 48,
        borderRadius: '50%',
        border: '1px solid var(--border)',
        background: 'var(--surface)',
        boxShadow: '0 4px 12px rgba(0,0,0,0.12)',
        cursor: 'pointer',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        opacity: visible ? 1 : 0,
        pointerEvents: visible ? 'auto' : 'none',
        transition: 'opacity 0.2s ease',
        zIndex: 50,
        color: 'var(--fg)',
        padding: 0,
      }}
    >
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <line x1="19" y1="12" x2="5" y2="12" />
        <polyline points="12 19 5 12 12 5" />
      </svg>
    </button>
  )
}
