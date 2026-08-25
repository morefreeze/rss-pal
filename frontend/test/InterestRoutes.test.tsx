import { render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AppRoutes } from '../src/App'

vi.mock('../src/api/client', async () => ({
  ...(await vi.importActual<typeof import('../src/api/client')>('../src/api/client')),
  isLoggedIn: vi.fn(() => true),
  getUnreadCount: vi.fn().mockResolvedValue(0),
  getServerHealth: vi.fn().mockResolvedValue({ status: 'ok', version: 'test-backend' }),
  getTopics: vi.fn().mockResolvedValue([]),
  getTags: vi.fn().mockResolvedValue([]),
  getLatestInterests: vi.fn().mockResolvedValue({
    interest: null,
    remaining_today: 3,
    remaining_month: 100,
  }),
}))

vi.mock('../src/pages/ArticleListPage', () => ({ default: () => <div>Article list</div> }))

function LocationProbe() {
  return <output data-testid="location">{useLocation().pathname}</output>
}

function renderRoute(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <AppRoutes
        user={{ id: 1, username: 'reader', is_admin: false }}
        onLogin={() => {}}
        onLogout={() => {}}
      />
      <LocationProbe />
    </MemoryRouter>,
  )
}

describe('interest routes', () => {
  it('keeps the canonical interests route', async () => {
    renderRoute('/interests')
    expect((await screen.findByTestId('location')).textContent).toBe('/interests')
  })

  it('redirects the legacy insights route to interests', async () => {
    renderRoute('/insights')
    expect((await screen.findByTestId('location')).textContent).toBe('/interests')
  })

  it('lets the removed recommended route fall through to articles', async () => {
    renderRoute('/recommended')
    expect((await screen.findByTestId('location')).textContent).toBe('/articles')
  })
})
