import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import LoginPage from '../src/pages/LoginPage'

const mocks = vi.hoisted(() => ({ methods: vi.fn(), login: vi.fn(), start: vi.fn() }))
vi.mock('../src/api/accountAuth', async importOriginal => ({
  ...await importOriginal<typeof import('../src/api/accountAuth')>(),
  getAuthMethods: mocks.methods,
  startAccountOAuth: mocks.start,
}))
vi.mock('../src/api/client', () => ({
  api: { post: vi.fn().mockRejectedValue(new Error('already initialized')) },
  login: mocks.login,
  getRegistrationConfig: vi.fn().mockResolvedValue({ available: true, provider: 'tencent', site_key: 'test' }),
}))
const methods = { public_registration: true, legacy_password: true, email: true, google: true, github: false, qq: true, apple: false }
beforeEach(() => {
  vi.clearAllMocks()
  mocks.methods.mockResolvedValue(methods)
  mocks.login.mockResolvedValue({ user: { id: 9, username: 'reader', is_admin: false } })
  mocks.start.mockRejectedValue(new Error('fixture stop'))
})
function view() {
  const onLogin = vi.fn()
  render(<MemoryRouter initialEntries={['/login?intent=capture&source=https%3A%2F%2Fexample.com%2Farticle']}>
    <Routes>
      <Route path="/login" element={<LoginPage onLogin={onLogin} />} />
      <Route path="/capture" element={<p>Capture destination</p>} />
    </Routes>
  </MemoryRouter>)
  return onLogin
}

it('replaces email login with password login and restores email when switched back', async () => {
  view()
  await screen.findByLabelText('邮箱')
  fireEvent.change(screen.getByLabelText('邮箱'), { target: { value: 'reader@example.com' } })
  expect(screen.queryByLabelText('用户名')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '使用账号密码登录' }))
  expect(screen.queryByLabelText('邮箱')).toBeNull()
  expect(screen.queryByLabelText('邮箱验证码')).toBeNull()
  expect(screen.queryByRole('button', { name: '发送验证码' })).toBeNull()
  expect(screen.queryByRole('button', { name: '验证并登录' })).toBeNull()
  expect(screen.getByLabelText('用户名')).toBeTruthy()
  expect(screen.getByLabelText('密码')).toBeTruthy()
  expect(screen.getAllByRole('checkbox', { name: '记住此设备 90 天' })).toHaveLength(1)
  expect(screen.getByRole('button', { name: '使用 Google 继续' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'QQ登录' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '使用邮箱验证码登录' }))
  expect((screen.getByLabelText('邮箱') as HTMLInputElement).value).toBe('reader@example.com')
  expect(screen.getByLabelText('邮箱验证码')).toBeTruthy()
  expect(screen.queryByLabelText('密码')).toBeNull()
  expect(screen.getAllByRole('checkbox', { name: '记住此设备 90 天' })).toHaveLength(1)
})

it('submits password login and keeps the requested destination after switching', async () => {
  const done = view()
  fireEvent.click(await screen.findByRole('button', { name: '使用账号密码登录' }))
  fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'reader' } })
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'test-password' } })
  fireEvent.click(screen.getByRole('checkbox', { name: '记住此设备 90 天' }))
  fireEvent.click(screen.getByRole('button', { name: '登录', exact: true }))
  await waitFor(() => expect(mocks.login).toHaveBeenCalledWith('reader', 'test-password', false))
  expect(await screen.findByText('Capture destination')).toBeTruthy()
  expect(done).toHaveBeenCalledWith(expect.objectContaining({ id: 9 }))
})

it('replaces the OAuth remember option when email login is unavailable', async () => {
  mocks.methods.mockResolvedValue({ ...methods, email: false })
  view()
  fireEvent.click(await screen.findByRole('button', { name: '使用账号密码登录' }))
  expect(screen.getAllByRole('checkbox', { name: '记住此设备 90 天' })).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: '使用第三方登录' }))
  expect(screen.queryByLabelText('密码')).toBeNull()
  expect(screen.getByRole('button', { name: '使用 Google 继续' })).toBeTruthy()
})

it('uses the visible remember choice for third-party login across form switches', async () => {
  view()
  fireEvent.click(await screen.findByRole('button', { name: '使用账号密码登录' }))
  fireEvent.click(screen.getByRole('checkbox', { name: '记住此设备 90 天' }))
  fireEvent.click(screen.getByRole('button', { name: '使用 Google 继续' }))
  await waitFor(() => expect(mocks.start).toHaveBeenCalledWith('google', expect.any(String), false, false))
  await screen.findByRole('alert')
  fireEvent.click(screen.getByRole('button', { name: '使用邮箱验证码登录' }))
  expect((screen.getByRole('checkbox', { name: '记住此设备 90 天' }) as HTMLInputElement).checked).toBe(false)
})
