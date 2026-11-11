import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, cleanup } from '@testing-library/react'
import { useState } from 'react'
import { Link, MemoryRouter, Outlet, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import ReadingWorkspace from './ReadingWorkspace'
import { useReadingWorkspace, WorkspaceSidebar, WorkspaceOverview } from './ReadingWorkspaceContext'
import MobileTabBar from '../components/MobileTabBar'

function MockList() {
  const [count, setCount] = useState(0)
  const location = useLocation()
  const navigate = useNavigate()
  const workspace = useReadingWorkspace()
  return <div data-testid="mock-list" data-active={workspace?.isActive} data-visible={workspace?.isListVisible}>
    <span>source {location.pathname}</span>
    <button onClick={() => workspace?.closeReader()}>apply scope</button>
    <button onClick={() => setCount(value => value + 1)}>count {count}</button>
    <button onClick={() => navigate('/articles/42', { state: { from: location.pathname + location.search } })}>open article</button>
    <WorkspaceSidebar><button>scope {location.pathname}</button></WorkspaceSidebar>
    <WorkspaceOverview><p>overview {location.pathname}</p></WorkspaceOverview>
  </div>
}
vi.mock('../pages/ArticleListPage', () => ({ default: () => <MockList /> }))
vi.mock('../pages/ClipPage', () => ({ default: () => <MockList /> }))
vi.mock('../pages/ExplorePage', () => ({ default: () => <MockList /> }))
vi.mock('../pages/DailyPage', () => ({ default: () => <MockList /> }))
vi.mock('../pages/WeeklyPage', () => ({ default: () => <MockList /> }))

function harness(path = '/articles', state?: { from: string }) {
  return render(<MemoryRouter initialEntries={[state ? { pathname: path, state } : path]}><Routes><Route element={<ReadingWorkspace navigation={<nav><Link to="/articles">articles nav</Link><Link to="/explore">explore nav</Link></nav>} account={null} footer={<footer>test filing footer</footer>}><Outlet /></ReadingWorkspace>}><Route path="/articles" element={<p>duplicate list</p>} /><Route path="/explore" element={<p>duplicate explore</p>} /><Route path="/articles/:id" element={<div>actual detail</div>} /><Route path="/history" element={<p>history page</p>} /><Route path="/settings" element={<p>settings content</p>} /></Route></Routes></MemoryRouter>)
}
function viewport(width: number) {
  Object.defineProperty(window, 'innerWidth', { value: width, configurable: true })
  window.matchMedia = vi.fn().mockImplementation(query => ({ matches: width <= Number(query.match(/\d+/)?.[0]), media: query, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
}
beforeEach(() => { viewport(1440); window.scrollTo = vi.fn(); sessionStorage.clear(); localStorage.clear() })
afterEach(cleanup)

describe('ReadingWorkspace', () => {
  it('preserves list state while opening and closing a reader', () => {
    harness()
    fireEvent.click(screen.getByText('count 0'))
    fireEvent.click(screen.getByText('open article'))
    expect(screen.getByText('actual detail')).toBeTruthy()
    expect(screen.getByText('count 1')).toBeTruthy()
    expect(screen.queryByText('duplicate list')).toBeNull()
    expect(screen.queryByText('overview /articles')).toBeNull()
    fireEvent.click(screen.getByText('← 返回列表'))
    expect(screen.queryByText('actual detail')).toBeNull()
    expect(screen.getByText('count 1')).toBeTruthy()
    expect(screen.getByText('overview /articles')).toBeTruthy()
  })
  it('keeps inactive lists mounted and suppresses their scope portals', () => {
    harness()
    fireEvent.click(screen.getByText('count 0'))
    fireEvent.click(screen.getByText('explore nav'))
    expect(screen.getAllByTestId('mock-list')).toHaveLength(2)
    expect(screen.queryByText('scope /articles')).toBeNull()
    expect(screen.getByText('scope /explore')).toBeTruthy()
    const inactive = screen.getByText('count 1').closest('.workspace-list') as HTMLDivElement
    expect(inactive.hidden).toBe(true)
    expect(inactive.inert).toBe(true)
    fireEvent.click(screen.getByText('articles nav'))
    expect(screen.getByText('count 1').closest('.workspace-list')?.hasAttribute('hidden')).toBe(false)
  })
  it('shows the full reader on phone and restores the list on return', () => {
    viewport(390)
    harness()
    expect(screen.getByText('overview /articles').closest('.workspace-inline-overview')).toBeTruthy()
    fireEvent.click(screen.getByText('open article'))
    expect((screen.getByTestId('mock-list').closest('.workspace-list') as HTMLDivElement).hidden).toBe(true)
    expect(screen.getByRole('main').hidden).toBe(false)
    fireEvent.click(screen.getByText('← 返回列表'))
    expect((screen.getByTestId('mock-list').closest('.workspace-list') as HTMLDivElement).hidden).toBe(false)
  })
  it('provides keyboard-close and focus restoration for tablet scopes', () => {
    viewport(900)
    harness()
    const trigger = screen.getByRole('button', { name: '☰ 导航与筛选' })
    trigger.focus()
    fireEvent.click(trigger)
    expect(screen.getByRole('dialog', { name: '导航与阅读范围' })).toBeTruthy()
    const windowEscape = vi.fn()
    window.addEventListener('keydown', windowEscape)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(windowEscape).not.toHaveBeenCalled()
    window.removeEventListener('keydown', windowEscape)
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(document.activeElement).toBe(trigger)
  })
  it('marks focused-reader and inactive module lists invisible without losing their state', () => {
    harness()
    fireEvent.click(screen.getByText('count 0'))
    fireEvent.click(screen.getByText('open article'))
    expect(screen.getByTestId('mock-list').getAttribute('data-visible')).toBe('true')
    fireEvent.click(screen.getByText('专注阅读'))
    expect(screen.getByTestId('mock-list').getAttribute('data-active')).toBe('true')
    expect(screen.getByTestId('mock-list').getAttribute('data-visible')).toBe('false')
    fireEvent.click(screen.getByText('退出专注'))
    expect(screen.getByTestId('mock-list').getAttribute('data-visible')).toBe('true')
    expect(screen.getByText('count 1')).toBeTruthy()
    fireEvent.click(screen.getByText('explore nav'))
    expect(screen.getByText('count 1').closest('[data-testid="mock-list"]')?.getAttribute('data-visible')).toBe('false')
  })
  it('clamps a persisted list width when a tablet viewport narrows', () => {
    viewport(900)
    localStorage.setItem('reading-workspace-list-width', '480')
    harness()
    expect(screen.getByRole('separator').getAttribute('aria-valuenow')).toBe('480')
    Object.defineProperty(window, 'innerWidth', { value: 768, configurable: true })
    fireEvent(window, new Event('resize'))
    expect(screen.getByRole('separator').getAttribute('aria-valuenow')).toBe('408')
    fireEvent.keyDown(screen.getByRole('separator'), { key: 'ArrowLeft' })
    expect(screen.getByRole('separator').getAttribute('aria-valuenow')).toBe('388')
  })
  it('renders one filing footer on phone lists and readers and returns focus', () => {
    viewport(390)
    harness()
    expect(screen.getAllByText('test filing footer')).toHaveLength(1)
    const opener = screen.getByText('open article')
    opener.focus()
    fireEvent.click(opener)
    expect(screen.getAllByText('test filing footer')).toHaveLength(1)
    fireEvent.click(screen.getByText('← 返回列表'))
    expect(document.activeElement).toBe(opener)
    expect(screen.getAllByText('test filing footer')).toHaveLength(1)
  })
  it('returns to the validated history origin while using articles as the virtual list', () => {
    harness('/articles/42', { from: '/history' })
    expect(screen.getByText('source /articles')).toBeTruthy()
    fireEvent.click(screen.getByText('← 返回历史'))
    expect(screen.getByText('history page')).toBeTruthy()
  })
  it('scope changes in a history-opened reader return to the virtual article list', () => {
    harness('/articles/42', { from: '/history' })
    fireEvent.click(screen.getByText('apply scope'))
    expect(screen.queryByText('actual detail')).toBeNull()
    expect(screen.queryByText('history page')).toBeNull()
    expect(screen.getByText('overview /articles')).toBeTruthy()
  })
  it('names the return to the same briefing issue', () => {
    harness('/articles/42', { from: '/weekly?week=2026-11-09' })
    expect(screen.getByText('← 返回本期简报')).toBeTruthy()
    expect(screen.getByText('source /weekly')).toBeTruthy()
  })
  it('keeps utility route content and navigation usable', () => {
    harness('/settings')
    expect(screen.getByText('settings content')).toBeTruthy()
    expect(screen.getByText('articles nav')).toBeTruthy()
    expect(screen.queryByTestId('mock-list')).toBeNull()
  })
})

describe('phone navigation', () => {
  it('uses the five approved tabs, marks briefings and offers subscription management in More', () => {
    render(<MemoryRouter initialEntries={['/weekly']}><MobileTabBar unreadCount={0} onLogout={() => {}} /></MemoryRouter>)
    expect(screen.getAllByRole('link').map(link => link.textContent?.replace(/[^\u4e00-\u9fff]/g, ''))).toEqual(['文章', '网摘', '探索', '简报'])
    expect(screen.getByRole('link', { name: /简报/ }).getAttribute('aria-current')).toBe('page')
    fireEvent.click(screen.getByRole('button', { name: /更多/ }))
    expect(screen.getByRole('dialog', { name: '更多' })).toBeTruthy()
    expect(screen.getByRole('button', { name: /订阅管理/ })).toBeTruthy()
  })
})
