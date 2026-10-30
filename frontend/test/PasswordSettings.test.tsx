import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import SettingsPage from '../src/pages/SettingsPage'

const mocks = vi.hoisted(() => ({ change: vi.fn() }))
vi.mock('../src/api/client', () => ({
  getTemplates: vi.fn().mockResolvedValue([]),
  changePassword: mocks.change,
}))
vi.mock('../src/components/NicknameSetup', () => ({ default: () => <p>Nickname settings</p> }))
vi.mock('../src/components/AccountLoginMethods', () => ({ default: () => null }))
const user = { id: 19, username: 'reader_auto', is_admin: false, needs_password: true }
beforeEach(() => {
  vi.clearAllMocks()
  window.location.hash = '#account'
  mocks.change.mockResolvedValue({ message: '密码已修改', user: { ...user, needs_password: false } })
})

it('allows an external account to set its first password without an old password', async () => {
  const updated = vi.fn()
  render(<MemoryRouter><SettingsPage user={user} onUserUpdate={updated} /></MemoryRouter>)
  expect(await screen.findByRole('heading', { name: '设置密码' })).toBeTruthy()
  expect(screen.queryByPlaceholderText('当前密码')).toBeNull()
  expect(screen.getByText(/登录用户名：reader_auto/)).toBeTruthy()
  fireEvent.change(screen.getByPlaceholderText('新密码（至少 6 位）'), { target: { value: 'first-password' } })
  fireEvent.click(screen.getByRole('button', { name: '设置密码' }))
  await waitFor(() => expect(mocks.change).toHaveBeenCalledWith('', 'first-password'))
  expect(updated).toHaveBeenCalledWith(expect.objectContaining({ id: 19, needs_password: false }))
  expect(await screen.findByText('密码已设置')).toBeTruthy()
})

it('keeps the current password required for accounts that already have one', async () => {
  render(<MemoryRouter><SettingsPage user={{ ...user, needs_password: false }} /></MemoryRouter>)
  await screen.findByPlaceholderText('当前密码')
  fireEvent.change(screen.getByPlaceholderText('新密码（至少 6 位）'), { target: { value: 'next-password' } })
  expect(screen.getByRole('button', { name: '修改密码' }).hasAttribute('disabled')).toBe(true)
  fireEvent.change(screen.getByPlaceholderText('当前密码'), { target: { value: 'old-password' } })
  fireEvent.click(screen.getByRole('button', { name: '修改密码' }))
  await waitFor(() => expect(mocks.change).toHaveBeenCalledWith('old-password', 'next-password'))
})
