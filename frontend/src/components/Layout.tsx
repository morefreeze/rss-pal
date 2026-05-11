import { useState, useEffect, useCallback } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import { logout, getUnreadCount } from '../api/client'
import Toaster from './Toaster'
import { PlayerProvider } from '../player/PlayerContext'
import MiniPlayer from './MiniPlayer'

interface LayoutProps {
  user: { id: number; username: string; is_admin: boolean } | null
  onLogout: () => void
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

  const articlesLabel = (
    <span style={{ position: 'relative', display: 'inline-flex', alignItems: 'center', gap: 4 }}>
      文章
      {unreadCount > 0 && (
        <span className="unread-badge">
          {unreadCount > 99 ? '99+' : unreadCount}
        </span>
      )}
    </span>
  )

  return (
    <PlayerProvider>
      <div>
      <header style={{ marginBottom: 20 }}>
        <div className="flex-between">
          <h1 className="nav-brand">RSS Pal</h1>

          <nav className="flex gap-2 desktop-nav" style={{ alignItems: 'center' }}>
            <NavLink to="/articles" className={navLinkClass}>{articlesLabel}</NavLink>
            <NavLink to="/weekly" className={navLinkClass}>周刊</NavLink>
            <NavLink to="/feeds" className={navLinkClass}>订阅</NavLink>
            <NavLink to="/recommended" className={navLinkClass}>推荐</NavLink>
            <NavLink to="/insights" className={navLinkClass}>洞察</NavLink>
            <NavLink to="/stats" className={navLinkClass}>统计</NavLink>
            <NavLink to="/settings" className={navLinkClass}>设置</NavLink>
            <span className="text-muted text-sm" style={{ borderLeft: '1px solid var(--border)', paddingLeft: 8 }}>
              {user?.username}
            </span>
            <button className="btn-ghost btn-sm" onClick={handleLogout}>
              登出
            </button>
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
            {[
              { to: '/articles', label: articlesLabel },
              { to: '/weekly', label: '周刊' },
              { to: '/feeds', label: '订阅' },
              { to: '/recommended', label: '推荐' },
              { to: '/insights', label: '洞察' },
              { to: '/stats', label: '统计' },
              { to: '/settings', label: '设置' },
            ].map(({ to, label }) => (
              <NavLink
                key={to}
                to={to}
                className={navLinkClass}
                onClick={() => setMenuOpen(false)}
                style={{ display: 'block', padding: '10px 16px', borderBottom: '1px solid var(--border)', borderRadius: 0 }}
              >
                {label}
              </NavLink>
            ))}
            <div style={{ padding: '10px 16px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span className="text-muted text-sm">{user?.username}</span>
              <button className="btn-ghost btn-sm" onClick={handleLogout}>
                登出
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
