import { useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import MoreSheet from './MoreSheet'

type Tab = { to: string; icon: string; label: string; showUnread?: boolean; matchClip?: boolean }

const TABS: Tab[] = [
  { to: '/articles',            icon: '📰', label: '文章', showUnread: true, matchClip: false },
  { to: '/articles?view=clip',  icon: '⭐', label: '网摘',                   matchClip: true  },
  { to: '/feeds',               icon: '📡', label: '订阅' },
]

interface Props {
  unreadCount: number
  onLogout: () => void
}

export default function MobileTabBar({ unreadCount, onLogout }: Props) {
  const [moreOpen, setMoreOpen] = useState(false)
  const location = useLocation()
  const isClipView = location.pathname === '/articles'
    && new URLSearchParams(location.search).get('view') === 'clip'

  const tabIsActive = (t: Tab) => {
    if (t.to === '/articles') {
      return location.pathname === '/articles' && !isClipView
    }
    if (t.matchClip) {
      return isClipView
    }
    // Other tabs (/feeds): plain pathname prefix match.
    return location.pathname === t.to || location.pathname.startsWith(t.to + '/')
  }

  const tabStyle = (active: boolean): React.CSSProperties => ({
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 2,
    padding: '6px 0',
    background: 'transparent',
    border: 'none',
    color: active ? 'var(--accent)' : 'var(--fg-muted)',
    fontSize: 11,
    fontWeight: 500,
    height: 'auto',
    textDecoration: 'none',
    minHeight: 44,
  })

  return (
    <>
      <nav
        className="mobile-tab-bar"
        aria-label="主导航"
        style={{
          position: 'fixed',
          left: 0, right: 0, bottom: 0,
          height: 'calc(56px + env(safe-area-inset-bottom))',
          paddingBottom: 'env(safe-area-inset-bottom)',
          background: 'var(--surface)',
          borderTop: '1px solid var(--border)',
          display: 'flex',
          zIndex: 1000,
        }}
      >
        {TABS.map(tab => {
          const active = tabIsActive(tab)
          return (
            <NavLink
              key={tab.to}
              to={tab.to}
              end={false}
              className="mobile-tab-link"
              style={tabStyle(active)}
            >
              <span style={{ fontSize: 22, lineHeight: 1, position: 'relative' }}>
                {tab.icon}
                {tab.showUnread && unreadCount > 0 && (
                  <span
                    className="unread-badge"
                    style={{ position: 'absolute', top: -4, right: -10 }}
                  >
                    {unreadCount > 99 ? '99+' : unreadCount}
                  </span>
                )}
              </span>
              <span>{tab.label}</span>
            </NavLink>
          )
        })}
        <button
          type="button"
          onClick={() => setMoreOpen(true)}
          aria-label="更多"
          style={tabStyle(moreOpen)}
        >
          <span style={{ fontSize: 22, lineHeight: 1 }}>⋯</span>
          <span>更多</span>
        </button>
      </nav>
      <MoreSheet open={moreOpen} onClose={() => setMoreOpen(false)} onLogout={onLogout} />
    </>
  )
}
