import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import Layout from '../src/components/Layout'
import ReadingHistoryPage from '../src/pages/ReadingHistoryPage'

const { getReadingHistory } = vi.hoisted(() => ({ getReadingHistory: vi.fn() }))
vi.mock('../src/api/client', async importOriginal => ({
  ...await importOriginal<typeof import('../src/api/client')>(),
  getReadingHistory,
  getUnreadCount: async () => 0,
  getServerHealth: async () => ({}),
}))

it('opens settings and history from the desktop account menu and loads older articles', async () => {
  const item = { id: 1, available: true, article_id: 12, title: '重新打开的网摘', kind: 'clip', source_title: '网摘', opened_at: '2026-11-09T01:00:00Z' }
  getReadingHistory
    .mockResolvedValueOnce({ items: [item], next_cursor: 'next' })
    .mockResolvedValueOnce({ items: [
      { ...item, id: 2, title: '看过的探索文章', kind: 'explore' },
      { ...item, id: 3, title: '已过期的探索文章', kind: 'explore', available: false, article_id: null },
    ], next_cursor: '' })

  render(<MemoryRouter initialEntries={['/articles']}>
    <Routes>
      <Route element={<Layout user={{ id: 1, username: 'reader', is_admin: false }} onLogout={() => {}} />}>
        <Route path="/articles" element={<div />} />
        <Route path="/settings" element={<h2>设置页面</h2>} />
        <Route path="/history" element={<ReadingHistoryPage />} />
      </Route>
    </Routes>
  </MemoryRouter>)

  expect(screen.queryByRole('link', { name: /设置/ })).toBeNull()
  const account = screen.getByRole('button', { name: '我的' })
  fireEvent.click(account)
  fireEvent.click(within(screen.getByRole('menu')).getByRole('menuitem', { name: '设置' }))
  expect(screen.getByRole('heading', { name: '设置页面' })).toBeTruthy()
  expect(screen.queryByRole('menu')).toBeNull()

  fireEvent.click(account)
  fireEvent.click(within(screen.getByRole('menu')).getByRole('menuitem', { name: '历史记录' }))
  expect((await screen.findByRole('link', { name: /重新打开的网摘/ })).getAttribute('href')).toBe('/articles/12')
  fireEvent.click(screen.getByRole('button', { name: '加载更多' }))
  expect((await screen.findByRole('link', { name: /看过的探索文章/ })).getAttribute('href')).toBe('/explore/articles/12')
  expect(screen.getByText('已过期的探索文章')).toBeTruthy()
  expect(screen.queryByRole('link', { name: /已过期的探索文章/ })).toBeNull()
  expect(screen.queryByRole('button', { name: '加载更多' })).toBeNull()
})
