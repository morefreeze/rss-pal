import { render, screen, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AppRoutes } from '../src/App'

vi.mock('../src/api/client', async () => ({
  ...(await vi.importActual<typeof import('../src/api/client')>('../src/api/client')),
  isLoggedIn: vi.fn(() => true),
  getBriefingLastTab: vi.fn().mockResolvedValue({ tab: 'weekly' }),
  getUnreadCount: vi.fn().mockResolvedValue(0),
  getServerHealth: vi.fn().mockResolvedValue({ status: 'ok', version: 'test-backend' }),
}))

vi.mock('../src/pages/ExplorePage', () => ({
  default: () => <div data-testid="explore-list-page">Explore list</div>,
}))

vi.mock('../src/pages/ExploreArticlePage', () => ({
  default: () => <div data-testid="explore-detail-page">Explore detail</div>,
}))

vi.mock('../src/pages/ArticleListPage', () => ({
  default: () => <div data-testid="articles-page">Article list</div>,
}))

vi.mock('../src/pages/DailyPage', () => ({ default: () => <div data-testid="daily-page" /> }))
vi.mock('../src/pages/WeeklyPage', () => ({ default: () => <div data-testid="weekly-page" /> }))

function PathProbe() {
  return <output data-testid="current-path">{useLocation().pathname}</output>
}

function renderRoute(path: string, isAdmin = true) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <PathProbe />
      <AppRoutes
        user={{ id: 1, username: 'reader', is_admin: isAdmin }}
        onboardingReady={false}
        onLogin={() => {}}
        onLogout={() => {}}
      />
    </MemoryRouter>,
  )
}

describe('explore routes and desktop navigation', () => {
  it('renders the authenticated explore list and exposes its desktop destination', async () => {
    renderRoute('/explore')

    expect(await screen.findByTestId('explore-list-page')).toBeTruthy()
    const desktopNav = document.querySelector('.workspace-nav') as HTMLElement
    const link = within(desktopNav).getByRole('link', { name: '探索' })
    expect(link.getAttribute('href')).toBe('/explore')
    expect(link.className).toContain('active')
  })

  it('renders the authenticated explore detail and keeps explore navigation active', async () => {
    renderRoute('/explore/articles/19')

    expect(await screen.findByTestId('explore-detail-page')).toBeTruthy()
    expect(screen.getByTestId('explore-list-page')).toBeTruthy()
    expect((screen.getByTestId('explore-list-page').closest('.workspace-list') as HTMLElement).hidden).toBe(false)
    const desktopNav = document.querySelector('.workspace-nav') as HTMLElement
    expect(within(desktopNav).getByRole('link', { name: '探索' }).className).toContain('active')
  })
})

it.each(['/explore', '/explore/articles/19', '/briefing', '/daily', '/weekly'])(
  'silently returns ordinary readers from %s and leaves no preview navigation',
  async (path) => {
    renderRoute(path, false)
    expect(await screen.findByTestId('articles-page')).toBeTruthy()
    expect(screen.getByTestId('current-path').textContent).toBe('/articles')
    expect(screen.queryByTestId('explore-list-page')).toBeNull()
    expect(screen.queryByTestId('explore-detail-page')).toBeNull()
    expect(screen.queryByRole('link', { name: '探索' })).toBeNull()
    expect(screen.queryByRole('link', { name: '简报' })).toBeNull()
  },
)

it.each([
  ['/briefing', '/weekly', 'weekly-page'],
  ['/daily', '/daily', 'daily-page'],
  ['/weekly', '/weekly', 'weekly-page'],
])('allows administrators to open %s', async (path, destination, page) => {
  renderRoute(path)
  expect(await screen.findByTestId(page)).toBeTruthy()
  expect(screen.getByTestId('current-path').textContent).toBe(destination)
  expect(screen.getByRole('link', { name: '简报' }).getAttribute('aria-current')).toBe('page')
})
