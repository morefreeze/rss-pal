import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import Layout from '../src/components/Layout'
import MoreSheet from '../src/components/MoreSheet'

vi.mock('../src/api/client', () => ({
  getServerHealth: vi.fn().mockResolvedValue({ status: 'ok', version: 'test-backend' }),
  getUnreadCount: vi.fn().mockResolvedValue(0),
}))

describe('interest navigation', () => {
  it('shows the interest link and removes the recommended link from desktop navigation', async () => {
    render(
      <MemoryRouter initialEntries={['/articles']}>
        <Routes>
          <Route element={<Layout user={{ id: 1, username: 'reader', is_admin: false }} onLogout={() => {}} />}>
            <Route path="/articles" element={<div>Articles</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    expect((await screen.findByRole('link', { name: /兴趣/ })).getAttribute('href')).toBe('/interests')
    expect(screen.queryByRole('link', { name: /推荐/ })).toBeNull()
  })

  it('shows the interest button and removes the recommended button from the mobile more sheet', () => {
    render(
      <MemoryRouter>
        <MoreSheet open onClose={() => {}} onLogout={() => {}} />
      </MemoryRouter>,
    )

    expect(screen.getByRole('button', { name: /兴趣/ })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /推荐/ })).toBeNull()
  })
})
