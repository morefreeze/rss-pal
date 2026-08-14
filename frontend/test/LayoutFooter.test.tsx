import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import Layout from '../src/components/Layout'

vi.mock('../src/api/client', () => ({
  getServerHealth: vi.fn().mockResolvedValue({ status: 'ok', version: 'test-backend' }),
  getUnreadCount: vi.fn().mockResolvedValue(0),
}))

describe('Layout footer', () => {
  it('links the ICP filing number to the MIIT filing system', async () => {
    render(
      <MemoryRouter initialEntries={['/articles']}>
        <Routes>
          <Route element={<Layout user={{ id: 1, username: 'reader', is_admin: false }} onLogout={() => {}} />}>
            <Route path="/articles" element={<div>Articles</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    const filingLink = await screen.findByRole('link', { name: '京ICP备2026025766号-2' })

    expect(filingLink.getAttribute('href')).toBe('https://beian.miit.gov.cn/')
  })
})
