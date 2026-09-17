import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Navigate, Route, Routes } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import AdminLayout from '../src/components/AdminLayout'
const state = vi.hoisted(() => ({ breakpoint: 'desktop' }))
vi.mock('../src/hooks/useBreakpoint', () => ({ useBreakpoint: () => state.breakpoint }))
beforeEach(() => { state.breakpoint = 'desktop' })
function mount(admin = true, path = '/admin') {
  return render(<MemoryRouter initialEntries={[path]}><Routes>
    <Route path="admin" element={<AdminLayout user={{is_admin: admin}} />}>
      <Route index element={<Navigate to="feed-catalog" replace />} />
      <Route path="feed-catalog" element={<p>目录内容</p>} />
      <Route path="monitoring" element={<p>监控内容</p>} />
    </Route>
  </Routes></MemoryRouter>)
}
it('后台默认目录并通过侧边栏切换，保留当前项高亮', () => {
  mount()
  expect(screen.getByText('目录内容')).toBeTruthy()
  expect(screen.getByRole('link', {name: /公共推荐目录/}).getAttribute('aria-current')).toBe('page')
  fireEvent.click(screen.getByRole('link', {name: /运行监控/}))
  expect(screen.getByText('监控内容')).toBeTruthy()
  expect(screen.getByRole('link', {name: /运行监控/}).getAttribute('aria-current')).toBe('page')
})
it('普通用户不能挂载后台内容', () => {
  mount(false, '/admin/monitoring')
  expect(screen.getByText('仅管理员可访问后台管理')).toBeTruthy()
  expect(screen.queryByText('监控内容')).toBeNull()
  expect(screen.queryByRole('navigation', {name: '后台导航'})).toBeNull()
})
it('手机展开后台菜单，选择后收起且切换内容', () => {
  state.breakpoint = 'phone';mount(true, '/admin/feed-catalog')
  const button=screen.getByRole('button',{name:/后台菜单/})
  expect(button.getAttribute('aria-expanded')).toBe('false')
  fireEvent.click(button)
  fireEvent.click(screen.getByRole('link',{name:/运行监控/}))
  expect(screen.getByText('监控内容')).toBeTruthy()
  expect(button.getAttribute('aria-expanded')).toBe('false')
})
