import { useState, useEffect, useRef, useCallback } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import { logout, getUnreadCount } from '../api/client'
import Toaster from './Toaster'
import { PlayerProvider } from '../player/PlayerContext'
import MiniPlayer from './MiniPlayer'

interface LayoutProps {
  user: { id: number; username: string; is_admin: boolean } | null
  onLogout: () => void
}

type NavItem = { to: string; icon: string; label: string }

const NAV_ITEMS: NavItem[] = [
  { to: '/articles',    icon: '📰', label: '文章' },
  { to: '/weekly',      icon: '📅', label: '周刊' },
  { to: '/feeds',       icon: '📡', label: '订阅' },
  { to: '/recommended', icon: '✨', label: '推荐' },
  { to: '/insights',    icon: '💡', label: '洞察' },
  { to: '/stats',       icon: '📊', label: '统计' },
  { to: '/settings',    icon: '⚙️', label: '设置' },
]

function UserMenu({ username, onLogout }: { username: string; onLogout: () => void }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', onClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <button
        type="button"
        className="nav-link"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(o => !o)}
        style={{
          background: open ? 'var(--surface-hover)' : 'transparent',
          border: 'none',
          height: 'auto',
          fontSize: 14,
          fontWeight: 400,
          display: 'inline-flex',
          alignItems: 'center',
          gap: 4,
          cursor: 'pointer',
        }}
      >
        <span>👤 {username}</span>
        <span style={{ fontSize: 10, opacity: 0.7 }}>▾</span>
      </button>
      {open && (
        <div
          role="menu"
          style={{
            position: 'absolute',
            right: 0,
            top: 'calc(100% + 6px)',
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            boxShadow: '0 6px 18px rgba(0,0,0,0.18)',
            minWidth: 140,
            padding: 4,
            zIndex: 100,
          }}
        >
          <button
            role="menuitem"
            type="button"
            onClick={() => { setOpen(false); onLogout() }}
            style={{
              width: '100%',
              textAlign: 'left',
              padding: '8px 12px',
              height: 'auto',
              background: 'transparent',
              color: 'var(--fg)',
              border: 'none',
              borderRadius: 4,
              fontWeight: 400,
              cursor: 'pointer',
            }}
          >
            🚪 登出
          </button>
        </div>
      )}
    </div>
  )
}

export default function Layout({ user, onLogout }: LayoutProps) {
  const [menuOpen, setMenuOpen] = useState(false)
  const [unreadCount, setUnreadCount] = useState(0)

  const refreshUnread = useCallback(() => {
    getUnreadCount().then(setUnreadCount).catch(() => {})
  }, [])

  useEffect(() => {
    refreshUnread()
    window.addEventListener('refresh-unread', refreshUnread)
    const interval = setInterval(refreshUnread, 2 * 60 * 1000)
    return () => {
      window.removeEventListener('refresh-unread', refreshUnread)
      clearInterval(interval)
    }
  }, [refreshUnread])

  const handleLogout = () => {
    logout()
    onLogout()
  }

  const navLinkClass = ({ isActive }: { isActive: boolean }) =>
    isActive ? 'nav-link active' : 'nav-link'

  const renderNavLabel = (item: NavItem) => {
    if (item.to !== '/articles' || unreadCount === 0) {
      return <>{item.icon} {item.label}</>
    }
    return (
      <span style={{ position: 'relative', display: 'inline-flex', alignItems: 'center', gap: 4 }}>
        {item.icon} {item.label}
        <span className="unread-badge">
          {unreadCount > 99 ? '99+' : unreadCount}
        </span>
      </span>
    )
  }

  return (
    <PlayerProvider>
      <div>
      <header style={{ marginBottom: 20 }}>
        <div className="flex-between">
          <h1 className="nav-brand">RSS Pal</h1>

          <nav className="flex gap-2 desktop-nav" style={{ alignItems: 'center' }}>
            {NAV_ITEMS.map(item => (
              <NavLink key={item.to} to={item.to} className={navLinkClass}>
                {renderNavLabel(item)}
              </NavLink>
            ))}
            {user && <UserMenu username={user.username} onLogout={handleLogout} />}
          </nav>

          <button
            className="btn-ghost btn-sm mobile-menu-btn"
            onClick={() => setMenuOpen(o => !o)}
            aria-label="菜单"
          >
            {menuOpen ? '✕' : '☰'}
          </button>
        </div>

        {menuOpen && (
          <nav className="mobile-nav" style={{
            marginTop: 8,
            padding: '8px 0',
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 8,
          }}>
            {NAV_ITEMS.map(item => (
              <NavLink
                key={item.to}
                to={item.to}
                className={navLinkClass}
                onClick={() => setMenuOpen(false)}
                style={{ display: 'block', padding: '10px 16px', borderBottom: '1px solid var(--border)', borderRadius: 0 }}
              >
                {renderNavLabel(item)}
              </NavLink>
            ))}
            <div style={{ padding: '10px 16px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span className="text-muted text-sm">👤 {user?.username}</span>
              <button className="btn-ghost btn-sm" onClick={handleLogout}>
                🚪 登出
              </button>
            </div>
          </nav>
        )}
      </header>
      <main style={{ paddingBottom: 80 }}>
        <Outlet />
      </main>
      <Toaster />
      <MiniPlayer />
    </div>
    </PlayerProvider>
  )
}
