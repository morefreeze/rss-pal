import UiIcon from '../components/UiIcon'
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { Link, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import ArticleListPage from '../pages/ArticleListPage'
import ClipPage from '../pages/ClipPage'
import ExplorePage from '../pages/ExplorePage'
import DailyPage from '../pages/DailyPage'
import WeeklyPage from '../pages/WeeklyPage'
import { useBreakpoint } from '../hooks/useBreakpoint'
import { ReadingWorkspaceContext, type ReadingWorkspaceValue } from './ReadingWorkspaceContext'
import { resolveWorkspaceRoute, type WorkspaceRoute } from './workspaceRoutes'
import './ReadingWorkspace.css'

const titles = { articles: '文章', clips: '网摘', explore: '探索', briefing: '简报' }
function ListRoutes({ path }: { path: string }) {
  return <Routes location={path}>
    <Route path="/articles" element={<ArticleListPage />} />
    <Route path="/clip" element={<ClipPage />} />
    <Route path="/explore" element={<ExplorePage />} />
    <Route path="/daily" element={<DailyPage />} />
    <Route path="/weekly" element={<WeeklyPage />} />
  </Routes>
}

export default function ReadingWorkspace({ children, navigation, account, footer }: {
  children: ReactNode; navigation: ReactNode; account: ReactNode; footer: ReactNode
}) {
  const location = useLocation()
  const navigate = useNavigate()
  const bp = useBreakpoint()
  const isPhone = bp === 'phone'
  const route = resolveWorkspaceRoute(location.pathname, location.search, location.state)
  const isReader = route?.articleId != null
  const [cached, setCached] = useState<Record<string, WorkspaceRoute>>(() => route ? { [route.cacheKey]: route } : {})
  // Update synchronously so opening a detail never mounts a duplicate list Outlet.
  if (route && cached[route.cacheKey]?.listPath !== route.listPath) {
    setCached(previous => ({ ...previous, [route.cacheKey]: route }))
  }
  const [sidebarTarget, setSidebarTarget] = useState<HTMLElement | null>(null)
  const [overviewTarget, setOverviewTarget] = useState<HTMLElement | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [focusMode, setFocusMode] = useState(false)
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth)
  const maxListWidth = Math.max(280, Math.min(480, viewportWidth - (bp === 'desktop' ? 220 : 0) - 360))
  const [listWidth, setListWidth] = useState(() => {
    try { return Math.min(480, Math.max(280, Number(localStorage.getItem('reading-workspace-list-width')) || 340)) } catch { return 340 }
  })
  const effectiveListWidth = Math.min(listWidth, maxListWidth)
  useEffect(() => {
    const resize = () => setViewportWidth(window.innerWidth)
    window.addEventListener('resize', resize)
    return () => window.removeEventListener('resize', resize)
  }, [])
  const listElements = useRef<Record<string, HTMLDivElement | null>>({})
  const phoneScroll = useRef<Record<string, number>>({})
  const listFocus = useRef<Record<string, HTMLElement | null>>({})
  const previousReader = useRef(false)
  const drawer = useRef<HTMLElement>(null)
  const reader = useRef<HTMLElement>(null)
  const activeKey = route?.cacheKey
  const openScopes = useCallback(() => setDrawerOpen(true), [])

  useEffect(() => {
    document.body.classList.add('reading-workspace-mounted')
    return () => document.body.classList.remove('reading-workspace-mounted')
  }, [])
  useEffect(() => { setDrawerOpen(false) }, [location.pathname, location.search])
  useEffect(() => { if (!isReader) setFocusMode(false) }, [isReader])
  useLayoutEffect(() => { if (reader.current) reader.current.inert = drawerOpen }, [drawerOpen])
  useLayoutEffect(() => { if (!isPhone && !isReader) window.scrollTo(0, 0) }, [isPhone, isReader, activeKey])
  useEffect(() => {
    if (!drawerOpen) return
    const previous = document.activeElement as HTMLElement | null
    const el = drawer.current
    const focusables = () => Array.from(el?.querySelectorAll<HTMLElement>('button, a[href], input, select, [tabindex="0"]') || [])
    focusables()[0]?.focus()
    const handleKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setDrawerOpen(false) }
      if (event.key !== 'Tab') return
      const elements = focusables().filter(item => !item.hasAttribute('disabled'))
      const first = elements[0], last = elements[elements.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
      if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
    }
    document.addEventListener('keydown', handleKey)
    const oldOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { document.removeEventListener('keydown', handleKey); document.body.style.overflow = oldOverflow; previous?.focus() }
  }, [drawerOpen])
  useEffect(() => {
    if (isReader) reader.current?.focus({ preventScroll: true })
    else if (previousReader.current && activeKey) listFocus.current[activeKey]?.focus({ preventScroll: true })
    previousReader.current = isReader
  }, [location.pathname, isReader, activeKey])
  // Window remains the reader scroller. Only phone lists use it, and retain their
  // own position independently from the reader's progress restoration.
  useLayoutEffect(() => {
    if (!isPhone || isReader || !activeKey) return
    window.scrollTo(0, phoneScroll.current[activeKey] || 0)
    const save = () => { phoneScroll.current[activeKey] = window.scrollY }
    window.addEventListener('scroll', save, { passive: true })
    return () => window.removeEventListener('scroll', save)
  }, [isPhone, isReader, activeKey])

  const setWidth = (width: number) => {
    const next = Math.round(Math.min(maxListWidth, Math.max(280, width)))
    setListWidth(next)
    try { localStorage.setItem('reading-workspace-list-width', String(next)) } catch { /* storage is optional */ }
  }
  const closeReader = () => { setDrawerOpen(false); if (route && isReader) navigate(route.returnPath) }
  const resetReaderToList = () => { setDrawerOpen(false); if (route && isReader) navigate(route.listPath) }
  const shared: ReadingWorkspaceValue | null = route ? {
    activeModule: route.module, isActive: true, isListVisible: !(isPhone && isReader) && !focusMode, isReaderOpen: isReader,
    selectedArticleId: route.articleId, isPhone, isCompact: !isPhone,
    listPath: route.listPath, returnPath: route.returnPath, getListScrollElement: () => isPhone ? null : listElements.current[route.cacheKey],
    closeReader, openScopes, sidebarTarget, overviewTarget,
  } : null
  return <div className={`reading-workspace ${route ? 'workspace-reading' : 'workspace-utility'}${isReader ? ' workspace-has-reader' : ''}${focusMode ? ' workspace-focus' : ''}`} style={{ '--workspace-list-width': `${effectiveListWidth}px` } as CSSProperties}>
    {drawerOpen && <div className="workspace-drawer-backdrop" onClick={() => setDrawerOpen(false)} />}
    <aside ref={drawer} className={`workspace-sidebar${drawerOpen ? ' workspace-sidebar-open' : ''}`} role={bp !== 'desktop' && drawerOpen ? 'dialog' : undefined} aria-modal={bp !== 'desktop' && drawerOpen ? true : undefined} aria-label="导航与阅读范围">
      <div className="workspace-brand"><Link to="/articles"><span className="workspace-brand-symbol" aria-hidden="true"><UiIcon name="rss" size={18} /></span>RSS Pal</Link><button className="btn-ghost btn-sm workspace-drawer-close" aria-label="关闭导航" onClick={() => setDrawerOpen(false)}><UiIcon name="close" /></button></div>
      {navigation}
      <div className="workspace-scopes" ref={setSidebarTarget} />
      <div className="workspace-account">{account}</div>
    </aside>
    <div className="workspace-mobile-header" ref={el => { if (el) el.inert = drawerOpen }}><span className="workspace-wordmark">RSS Pal</span><button className="btn-ghost btn-sm" onClick={openScopes} aria-haspopup="dialog" aria-expanded={drawerOpen}><UiIcon name="menu" /> 导航与筛选</button></div>
    {Object.entries(cached).map(([key, saved]) => {
      const active = key === activeKey
      const hidden = !active || (isPhone && isReader) || focusMode
      const value: ReadingWorkspaceValue = { ...shared!, activeModule: route?.module || saved.module, isActive: active, isListVisible: !hidden, isReaderOpen: active && isReader, selectedArticleId: active ? route?.articleId ?? null : null,
        isPhone, isCompact: !isPhone, listPath: saved.listPath, returnPath: active && route ? route.returnPath : saved.listPath, getListScrollElement: () => isPhone ? null : listElements.current[key], closeReader: active ? resetReaderToList : () => {}, openScopes, sidebarTarget, overviewTarget }
      return <div key={key} ref={el => { listElements.current[key] = el; if (el) el.inert = hidden || drawerOpen }} hidden={hidden} onFocusCapture={event => { listFocus.current[key] = event.target as HTMLElement }} onClickCapture={event => {
        const target = (event.target as HTMLElement).closest<HTMLElement>('a[href], button, [tabindex]')
        if (target) listFocus.current[key] = target
      }} className="workspace-list" aria-label={`${titles[saved.module]}列表`}>
        <ReadingWorkspaceContext.Provider value={value}><ListRoutes path={saved.listPath} /></ReadingWorkspaceContext.Provider>
        {active && isPhone && !isReader && footer}
      </div>
    })}
    {route && !focusMode && <div role="separator" tabIndex={isPhone ? -1 : 0} aria-label="调整文章列表宽度" aria-orientation="vertical" aria-valuemin={280} aria-valuemax={maxListWidth} aria-valuenow={effectiveListWidth} className="workspace-divider" onKeyDown={event => {
      if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); setWidth(effectiveListWidth + (event.key === 'ArrowRight' ? 20 : -20)) }
      if (event.key === 'Home') { event.preventDefault(); setWidth(280) }
      if (event.key === 'End') { event.preventDefault(); setWidth(480) }
    }} onPointerDown={event => { event.currentTarget.focus(); event.currentTarget.setPointerCapture(event.pointerId); event.preventDefault() }} onPointerMove={event => { if (event.currentTarget.hasPointerCapture(event.pointerId)) setWidth(event.clientX - (bp === 'desktop' ? 220 : 0)) }} onPointerUp={event => { if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId) }} />}
    <main ref={reader} tabIndex={-1} className="workspace-reader" aria-label={isReader ? '文章阅读器' : route ? '阅读概览' : '页面内容'} hidden={!!route && isPhone && !isReader}>
      {isReader && <div className="workspace-reader-toolbar"><button className="btn-ghost btn-sm" onClick={closeReader}>← {route?.returnPath === '/history' ? '返回历史' : route?.module === 'briefing' ? '返回本期简报' : '返回列表'}</button><button className="btn-ghost btn-sm workspace-focus-toggle" aria-pressed={focusMode} onClick={() => setFocusMode(value => !value)}>{focusMode ? '退出专注' : '专注阅读'}</button></div>}
      <ReadingWorkspaceContext.Provider value={shared}>
        {(!route || isReader) && <div className="workspace-page">{children}{footer}</div>}
        {route && !isReader && <><div className="workspace-overview" ref={setOverviewTarget}><div className="workspace-empty-reader"><span className="workspace-empty-mark" aria-hidden="true"><UiIcon name="book" size={40} /></span><h2>选择一篇文章</h2><p>在列表中选择文章后，这里显示正文。</p></div></div>{!isPhone && footer}</>}
      </ReadingWorkspaceContext.Provider>
    </main>
  </div>
}
