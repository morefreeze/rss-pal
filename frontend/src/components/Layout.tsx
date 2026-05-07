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
    // Poll every 2 minutes so the badge stays current as worker fetches articles
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
        <span style={{
          background: '#0066cc',
          color: 'white',
          borderRadius: 10,
          fontSize: 11,
          fontWeight: 600,
          padding: '1px 5px',
          minWidth: 18,
          textAlign: 'center',
          lineHeight: '16px',
        }}>
          {unreadCount > 99 ? '99+' : unreadCount}
        </span>
      )}
    </span>
  )

  return (
    <PlayerProvider>
      <div>
      <header style={{ marginBottom: 16 }}>
        <div className="flex-between">
          <h1 style={{ fontSize: 20, fontWeight: 700, color: '#0066cc' }}>RSS Pal</h1>

          {/* Desktop nav */}
          <nav className="flex gap-2 desktop-nav" style={{ alignItems: 'center' }}>
            <NavLink to="/articles" className={navLinkClass}>{articlesLabel}</NavLink>
            <NavLink to="/weekly" className={navLinkClass}>周刊</NavLink>
            <NavLink to="/feeds" className={navLinkClass}>订阅</NavLink>
            <NavLink to="/recommended" className={navLinkClass}>推荐</NavLink>
            <NavLink to="/insights" className={navLinkClass}>洞察</NavLink>
            <NavLink to="/stats" className={navLinkClass}>统计</NavLink>
            <NavLink to="/settings" className={navLinkClass}>设置</NavLink>
            <span className="text-muted text-sm" style={{ borderLeft: '1px solid #ddd', paddingLeft: 8 }}>
              {user?.username}
            </span>
            <button className="secondary" onClick={handleLogout} style={{ padding: '4px 10px', fontSize: 13 }}>
              登出
            </button>
          </nav>

          {/* Mobile menu button */}
          <button
            className="secondary mobile-menu-btn"
            onClick={() => setMenuOpen(o => !o)}
            style={{ padding: '4px 10px', fontSize: 18 }}
          >
            {menuOpen ? '✕' : '☰'}
          </button>
        </div>

        {/* Mobile dropdown nav */}
        {menuOpen && (
          <nav className="mobile-nav" style={{ marginTop: 8, padding: '8px 0', background: 'white', borderRadius: 8, boxShadow: '0 2px 8px rgba(0,0,0,0.12)' }}>
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
                style={{ display: 'block', padding: '10px 16px', borderBottom: '1px solid #f0f0f0' }}
              >
                {label}
              </NavLink>
            ))}
            <div style={{ padding: '10px 16px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span className="text-muted text-sm">{user?.username}</span>
              <button className="secondary" onClick={handleLogout} style={{ padding: '4px 10px', fontSize: 13 }}>
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
