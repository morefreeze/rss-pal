import { useState, useEffect, useRef, useCallback } from 'react'
import { Link, Outlet, useLocation } from 'react-router-dom'
import { logout, getUnreadCount, getServerHealth } from '../api/client'
import Toaster from './Toaster'
import { PlayerProvider, usePlayer } from '../player/PlayerContext'
import MiniPlayer from './MiniPlayer'
import MobileTabBar from './MobileTabBar'
import { useBreakpoint } from '../hooks/useBreakpoint'
import { VERSION as FRONTEND_VERSION } from '../version'

function VersionFooter() {
  const [backend, setBackend] = useState<string>('-')
  useEffect(() => {
    getServerHealth()
      .then(h => setBackend(h.version || '-'))
      .catch(() => setBackend('?'))
  }, [])
  return (
    <footer
      className="version-footer text-muted"
      style={{
        fontSize: 12,
        textAlign: 'center',
        padding: '16px 0',
        marginBottom: 'var(--bottom-chrome, 0px)',
        opacity: 0.65,
      }}
    >
      frontend {FRONTEND_VERSION} · backend {backend}
    </footer>
  )
}

interface LayoutProps {
  user: { id: number; username: string; is_admin: boolean } | null
  onLogout: () => void
}

// matchClip flips the active predicate so the 网摘 tab matches when
// /articles?view=clip is in the URL, and the regular 文章 tab does NOT
// match in that case. Without this, both share pathname /articles and
// React Router's default isActive would light up both.
type NavItem = { to: string; icon: string; label: string; matchClip?: boolean }

const NAV_ITEMS: NavItem[] = [
  { to: '/articles',           icon: '📰', label: '文章' },
  { to: '/articles?view=clip', icon: '⭐', label: '网摘', matchClip: true },
  { to: '/feeds',              icon: '📡', label: '订阅' },
  { to: '/briefing',           icon: '📅', label: '简报' },
  { to: '/recommended',        icon: '✨', label: '推荐' },
  { to: '/insights',           icon: '💡', label: '洞察' },
  { to: '/stats',              icon: '📊', label: '统计' },
  { to: '/settings',           icon: '⚙️', label: '设置' },
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
        <span
          aria-hidden="true"
          style={{
            fontSize: 14,
            lineHeight: 1,
            opacity: 0.85,
            transform: open ? 'rotate(180deg)' : 'none',
            transition: 'transform 0.15s ease',
          }}
        >
          ▾
        </span>
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
      <LayoutInner
        user={user}
        unreadCount={unreadCount}
        onLogout={handleLogout}
        renderNavLabel={renderNavLabel}
        menuOpen={menuOpen}
        setMenuOpen={setMenuOpen}
      />
    </PlayerProvider>
  )
}

interface LayoutInnerProps {
  user: { id: number; username: string; is_admin: boolean } | null
  unreadCount: number
  onLogout: () => void
  renderNavLabel: (item: NavItem) => React.ReactNode
  menuOpen: boolean
  setMenuOpen: (v: boolean) => void
}

function LayoutInner({
  user, unreadCount, onLogout, renderNavLabel, menuOpen, setMenuOpen,
}: LayoutInnerProps) {
  const bp = useBreakpoint()
  const player = usePlayer()
  const location = useLocation()

  // /articles?view=clip is the 网摘 view embedded inside the article page.
  // Two desktop tabs share pathname /articles, so React Router's default
  // active predicate can't distinguish them — we derive activeness from
  // (pathname + search) instead.
  const isClipView =
    location.pathname === '/articles' &&
    new URLSearchParams(location.search).get('view') === 'clip'

  const itemIsActive = (item: NavItem): boolean => {
    if (item.to === '/articles') return location.pathname === '/articles' && !isClipView
    if (item.matchClip) return isClipView
    return location.pathname === item.to || location.pathname.startsWith(item.to + '/')
  }

  const navLinkClass = (item: NavItem) => (itemIsActive(item) ? 'nav-link active' : 'nav-link')

  // --bottom-chrome = tab-bar height (if shown) + mini-player height (if active)
  // + safe-area-inset-bottom + 16px gutter. Reads on <body> so any deep main
  // can pad correctly.
  useEffect(() => {
    const tabH = bp === 'desktop' ? 0 : 56
    const playerH = player.articleId !== null ? 64 : 0
    const chrome = `calc(${tabH + playerH + 16}px + env(safe-area-inset-bottom))`
    document.body.style.setProperty('--bottom-chrome', chrome)
    return () => {
      document.body.style.removeProperty('--bottom-chrome')
    }
  }, [bp, player.articleId])

  return (
    <div>
      <header style={{ marginBottom: 20 }}>
        <div className="flex-between">
          <h1 className="nav-brand">RSS Pal</h1>

          <nav className="flex gap-2 desktop-nav" style={{ alignItems: 'center' }}>
            {NAV_ITEMS.map(item => (
              <Link key={item.to} to={item.to} className={navLinkClass(item)}>
                {renderNavLabel(item)}
              </Link>
            ))}
            {user && <UserMenu username={user.username} onLogout={onLogout} />}
          </nav>

          <button
            className="btn-ghost btn-sm mobile-menu-btn"
            onClick={() => setMenuOpen(!menuOpen)}
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
              <Link
                key={item.to}
                to={item.to}
                className={navLinkClass(item)}
                onClick={() => setMenuOpen(false)}
                style={{ display: 'block', padding: '10px 16px', borderBottom: '1px solid var(--border)', borderRadius: 0 }}
              >
                {renderNavLabel(item)}
              </Link>
            ))}
            <div style={{ padding: '10px 16px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span className="text-muted text-sm">👤 {user?.username}</span>
              <button className="btn-ghost btn-sm" onClick={onLogout}>
                🚪 登出
              </button>
            </div>
          </nav>
        )}
      </header>
      <main>
        <Outlet />
      </main>
      <VersionFooter />
      <Toaster />
      <MiniPlayer />
      {bp !== 'desktop' && (
        <MobileTabBar unreadCount={unreadCount} onLogout={onLogout} />
      )}
    </div>
  )
}
