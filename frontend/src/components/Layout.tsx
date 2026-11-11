import AccountDisplay, { type DisplayAccount } from './AccountDisplay'
import { useState, useEffect, useRef, useCallback } from 'react'
import { Link, Outlet, useLocation } from 'react-router-dom'
import { getUnreadCount, getServerHealth } from '../api/client'
import Toaster from './Toaster'
import { PlayerProvider, usePlayer } from '../player/PlayerContext'
import MiniPlayer from './MiniPlayer'
import MobileTabBar from './MobileTabBar'
import { useBreakpoint } from '../hooks/useBreakpoint'
import ReadingWorkspace from '../workspace/ReadingWorkspace'
import { resolveWorkspaceRoute } from '../workspace/workspaceRoutes'
import { readWorkspaceDestinations, rememberWorkspaceDestination, moduleDestination } from '../workspace/workspaceDestinations'
import { VERSION as FRONTEND_VERSION } from '../version'

const ICP_FILING_NUMBER = '京ICP备2026025766号-2'
const ICP_FILING_URL = 'https://beian.miit.gov.cn/'

function VersionFooter({ backend }: { backend: string }) {
  return (
    <footer className="version-footer text-muted">
      <div className="version-footer-meta">
        frontend {FRONTEND_VERSION} · backend {backend}
      </div>
      <div className="version-footer-filings">
        <a
          className="filing-link"
          href={ICP_FILING_URL}
          target="_blank"
          rel="noreferrer"
        >
          {ICP_FILING_NUMBER}
        </a>
      </div>
    </footer>
  )
}

interface LayoutProps {
  user: (DisplayAccount & { id: number; is_admin: boolean }) | null
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
  { to: '/explore',            icon: '🔭', label: '探索' },
  { to: '/briefing',           icon: '📅', label: '简报' },
  { to: '/feeds',              icon: '📡', label: '订阅' },
  { to: '/interests',          icon: '💡', label: '兴趣' },
  { to: '/stats',              icon: '📊', label: '统计' },
]

function UserMenu({ user, onLogout }: { user: DisplayAccount; onLogout: () => void }) {
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
    <div ref={ref} className="user-menu" style={{ position: 'relative' }}>
      <button
        type="button"
        className="nav-link"
        aria-label="我的"
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
        <AccountDisplay user={user} label="我的" />
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
          <Link role="menuitem" className="user-menu-item" to="/history" onClick={() => setOpen(false)}>
            历史记录
          </Link>
          <Link role="menuitem" className="user-menu-item" to="/settings" onClick={() => setOpen(false)}>
            设置
          </Link>
          <button
            role="menuitem"
            className="user-menu-item"
            type="button"
            onClick={() => { setOpen(false); onLogout() }}
          >
            🚪 登出
          </button>
        </div>
      )}
    </div>
  )
}

export default function Layout({ user, onLogout }: LayoutProps) {
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
      />
    </PlayerProvider>
  )
}

interface LayoutInnerProps {
  user: (DisplayAccount & { id: number; is_admin: boolean }) | null
  unreadCount: number
  onLogout: () => void
  renderNavLabel: (item: NavItem) => React.ReactNode
}

function LayoutInner({ user, unreadCount, onLogout, renderNavLabel }: LayoutInnerProps) {
  const bp = useBreakpoint()
  const player = usePlayer()
  const location = useLocation()
  const [backendVersion, setBackendVersion] = useState('-')
  useEffect(() => { getServerHealth().then(health => setBackendVersion(health.version || '-')).catch(() => setBackendVersion('?')) }, [])
  const workspaceRoute = resolveWorkspaceRoute(location.pathname, location.search, location.state)
  const destinations = readWorkspaceDestinations(user?.id)
  if (workspaceRoute && workspaceRoute.articleId == null) destinations[workspaceRoute.module] = workspaceRoute.listPath
  useEffect(() => {
    if (workspaceRoute && workspaceRoute.articleId == null) rememberWorkspaceDestination(user?.id, workspaceRoute.module, workspaceRoute.listPath)
  }, [user?.id, workspaceRoute?.module, workspaceRoute?.listPath, workspaceRoute?.articleId])
  const showTabs = bp === 'phone' && workspaceRoute?.articleId == null
  const navItems = user?.is_admin ? [...NAV_ITEMS, { to: '/admin', icon: '🛡️', label: '后台管理' }] : NAV_ITEMS
  const itemIsActive = (item: NavItem) => {
    if (item.to === '/articles') return workspaceRoute?.module === 'articles'
    if (item.matchClip) return workspaceRoute?.module === 'clips'
    if (item.to === '/explore') return workspaceRoute?.module === 'explore'
    if (item.to === '/briefing') return workspaceRoute?.module === 'briefing' || location.pathname === '/briefing'
    return location.pathname === item.to || location.pathname.startsWith(item.to + '/')
  }
  useEffect(() => {
    const tabH = showTabs ? 56 : 0
    const playerH = player.articleId !== null ? 64 : 0
    document.body.style.setProperty('--bottom-chrome', `calc(${tabH + playerH + 16}px + env(safe-area-inset-bottom))`)
    document.body.style.setProperty('--mobile-tabs-offset', showTabs ? 'calc(56px + env(safe-area-inset-bottom))' : '0px')
    return () => { document.body.style.removeProperty('--bottom-chrome'); document.body.style.removeProperty('--mobile-tabs-offset') }
  }, [showTabs, player.articleId])
  const navigation = <nav className="workspace-nav" aria-label="主导航">{navItems.slice(0, 4).map(item => <Link key={item.to} to={moduleDestination(item.to, destinations)} className={`nav-link${itemIsActive(item) ? ' active' : ''}`} aria-current={itemIsActive(item) ? 'page' : undefined}>{renderNavLabel(item)}</Link>)}</nav>
  const account = <><details className="workspace-more"><summary>更多</summary><nav className="workspace-nav" aria-label="更多导航">{navItems.slice(4).map(item => <Link key={item.to} to={item.to} className={`nav-link${itemIsActive(item) ? ' active' : ''}`} aria-current={itemIsActive(item) ? 'page' : undefined}>{renderNavLabel(item)}</Link>)}</nav></details>{user && <UserMenu user={user} onLogout={onLogout} />}</>
  return <>
    <ReadingWorkspace navigation={navigation} account={account} footer={<VersionFooter backend={backendVersion} />}><Outlet /></ReadingWorkspace>
    <Toaster />
    <MiniPlayer />
    {showTabs && <MobileTabBar destinations={destinations} user={user} unreadCount={unreadCount} onLogout={onLogout} isAdmin={!!user?.is_admin} />}
  </>
}
