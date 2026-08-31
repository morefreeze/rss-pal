import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AppRoutes } from '../src/App'

vi.mock('../src/api/client', async () => ({
  ...(await vi.importActual<typeof import('../src/api/client')>('../src/api/client')),
  isLoggedIn: vi.fn(() => true),
  getUnreadCount: vi.fn().mockResolvedValue(0),
  getServerHealth: vi.fn().mockResolvedValue({ status: 'ok', version: 'test-backend' }),
}))

vi.mock('../src/pages/ExplorePage', () => ({
  default: () => <div data-testid="explore-list-page">Explore list</div>,
}))

vi.mock('../src/pages/ExploreArticlePage', () => ({
  default: () => <div data-testid="explore-detail-page">Explore detail</div>,
}))

function renderRoute(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AppRoutes
        user={{ id: 1, username: 'reader', is_admin: false }}
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
    const desktopNav = document.querySelector('.desktop-nav') as HTMLElement
    const link = within(desktopNav).getByRole('link', { name: '🔭 探索' })
    expect(link.getAttribute('href')).toBe('/explore')
    expect(link.className).toContain('active')
  })

  it('renders the authenticated explore detail and keeps explore navigation active', async () => {
    renderRoute('/explore/articles/19')

    expect(await screen.findByTestId('explore-detail-page')).toBeTruthy()
    expect(screen.queryByTestId('explore-list-page')).toBeNull()
    const desktopNav = document.querySelector('.desktop-nav') as HTMLElement
    expect(within(desktopNav).getByRole('link', { name: '🔭 探索' }).className).toContain('active')
  })
})
