import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import VerifiedAccountAccess from '../src/components/VerifiedAccountAccess'
const mocks = vi.hoisted(() => ({
  send: vi.fn(),
  verify: vi.fn(),
  start: vi.fn(),
  persist: vi.fn(),
  captcha: vi.fn(),
  owner: vi.fn(),
}))
vi.mock('../src/api/accountAuth', () => ({
  requestEmailCode: mocks.send,
  verifyEmailCode: mocks.verify,
  startAccountOAuth: mocks.start,
  persistAccountSession: mocks.persist,
  currentAccountID: mocks.owner,
}))
vi.mock('../src/api/client', () => ({
  getRegistrationConfig: vi.fn(async () => ({
    available: true,
    provider: 'tencent',
    site_key: 'test',
  })),
}))
vi.mock('../src/utils/tencentCaptcha', () => ({
  verifyTencentCaptcha: mocks.captcha,
}))
const methods = {
  public_registration: true,
  legacy_password: true,
  email: true,
  google: true,
  apple: false,
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.owner.mockReturnValue(null)
  localStorage.clear()
  sessionStorage.clear()
  mocks.captcha.mockResolvedValue('proof')
  mocks.send.mockResolvedValue({ challenge_id: 'challenge', retry_after: 60 })
  mocks.verify.mockResolvedValue({
    token: 'application-token',
    user: { id: 9, username: 'new', is_admin: false },
  })
})
it('registers with an emailed code without requiring invite or password', async () => {
  const done = vi.fn()
  render(
    <VerifiedAccountAccess
      methods={methods}
      mode="register"
      returnTo="/articles"
      onSuccess={done}
    />,
  )
  fireEvent.change(screen.getByLabelText('邮箱'), {
    target: { value: 'reader@example.com' },
  })
  await waitFor(() =>
    expect(
      screen
        .getByRole('button', { name: '发送验证码' })
        .hasAttribute('disabled'),
    ).toBe(false),
  )
  fireEvent.click(screen.getByRole('button', { name: '发送验证码' }))
  await waitFor(() =>
    expect(mocks.send).toHaveBeenCalledWith(
      'reader@example.com',
      'proof',
      false,
    ),
  )
  fireEvent.change(screen.getByLabelText('邮箱验证码'), {
    target: { value: '123456' },
  })
  fireEvent.click(screen.getByRole('button', { name: '验证并注册' }))
  await waitFor(() =>
    expect(done).toHaveBeenCalledWith(expect.objectContaining({ id: 9 })),
  )
  expect(mocks.verify).toHaveBeenCalledWith('challenge', '123456', true, false)
  expect(mocks.persist).toHaveBeenCalled()
  expect(screen.queryByPlaceholderText('邀请码')).toBeNull()
  expect(screen.queryByLabelText('密码')).toBeNull()
})
it('does not render an unavailable Apple login and preserves the return destination', async () => {
  render(
    <VerifiedAccountAccess
      methods={methods}
      mode="login"
      returnTo="/feeds?add=1&source=https%3A%2F%2Fexample.com%2Frss"
      onSuccess={vi.fn()}
    />,
  )
  expect(screen.queryByRole('button', { name: '使用 Apple 继续' })).toBeNull()
  mocks.start.mockRejectedValue(new Error('offline'))
  fireEvent.click(screen.getByRole('button', { name: '使用 Google 继续' }))
  await waitFor(() =>
    expect(mocks.start).toHaveBeenCalledWith(
      'google',
      expect.stringContaining('/feeds?'),
      true,
      false,
    ),
  )
  expect(await screen.findByRole('alert')).toBeTruthy()
})
it('invalidates a challenge when the address changes and keeps errors retryable', async () => {
  render(
    <VerifiedAccountAccess
      methods={methods}
      mode="login"
      returnTo="/articles"
      onSuccess={vi.fn()}
    />,
  )
  fireEvent.change(screen.getByLabelText('邮箱'), {
    target: { value: 'one@example.com' },
  })
  await waitFor(() =>
    expect(
      screen
        .getByRole('button', { name: '发送验证码' })
        .hasAttribute('disabled'),
    ).toBe(false),
  )
  fireEvent.click(screen.getByRole('button', { name: '发送验证码' }))
  await waitFor(() => expect(mocks.send).toHaveBeenCalled())
  fireEvent.change(screen.getByLabelText('邮箱'), {
    target: { value: 'two@example.com' },
  })
  fireEvent.change(screen.getByLabelText('邮箱验证码'), {
    target: { value: '123456' },
  })
  fireEvent.click(screen.getByRole('button', { name: '验证并登录' }))
  expect(mocks.verify).not.toHaveBeenCalled()
})

it('lets OAuth-only users opt out of remembered sessions', async () => {
  render(
    <VerifiedAccountAccess
      methods={{ ...methods, email: false }}
      mode="login"
      returnTo="/articles"
    />,
  )
  fireEvent.click(screen.getByLabelText('记住此设备 90 天'))
  mocks.start.mockRejectedValue(new Error('offline'))
  fireEvent.click(screen.getByRole('button', { name: '使用 Google 继续' }))
  await waitFor(() =>
    expect(mocks.start).toHaveBeenCalledWith(
      'google',
      '/articles',
      false,
      false,
    ),
  )
})
it('binds the existing owner without replacing their session and allows retry after wrong code', async () => {
  mocks.owner.mockReturnValue(7)
  const bound = vi.fn()
  render(
    <VerifiedAccountAccess
      methods={methods}
      mode="bind"
      returnTo="/settings#account"
      onBound={bound}
    />,
  )
  fireEvent.change(screen.getByLabelText('邮箱'), {
    target: { value: 'owner@example.com' },
  })
  fireEvent.click(screen.getByRole('button', { name: '发送验证码' }))
  await waitFor(() =>
    expect(mocks.send).toHaveBeenCalledWith('owner@example.com', '', true),
  )
  fireEvent.change(screen.getByLabelText('邮箱验证码'), {
    target: { value: '123456' },
  })
  mocks.verify.mockRejectedValueOnce({
    response: { status: 400, data: { error: '验证码无效或已过期' } },
  })
  fireEvent.click(screen.getByRole('button', { name: '验证并绑定' }))
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(mocks.verify).toHaveBeenCalledTimes(1)
  expect(mocks.persist).not.toHaveBeenCalled()
  mocks.verify.mockResolvedValue({ bound: true })
  fireEvent.click(screen.getByRole('button', { name: '验证并绑定' }))
  await waitFor(() => expect(bound).toHaveBeenCalledTimes(1))
  expect(mocks.persist).not.toHaveBeenCalled()
})
it('rejects email binding after switching accounts between send and verify', async () => {
  mocks.owner.mockReturnValue(7)
  render(
    <VerifiedAccountAccess
      methods={methods}
      mode="bind"
      returnTo="/settings#account"
    />,
  )
  fireEvent.change(screen.getByLabelText('邮箱'), {
    target: { value: 'owner@example.com' },
  })
  fireEvent.click(screen.getByRole('button', { name: '发送验证码' }))
  await waitFor(() => expect(mocks.send).toHaveBeenCalled())
  mocks.owner.mockReturnValue(8)
  fireEvent.change(screen.getByLabelText('邮箱验证码'), {
    target: { value: '123456' },
  })
  fireEvent.click(screen.getByRole('button', { name: '验证并绑定' }))
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(mocks.verify).not.toHaveBeenCalled()
})

it('offers GitHub login without enabling email or unrelated providers', () => {
 render(<VerifiedAccountAccess methods={{...methods, email:false, google:false, github:true}} mode="login" returnTo="/articles" />)
 expect(screen.getByRole('button', {name:'使用 GitHub 继续'})).toBeTruthy()
 expect(screen.queryByLabelText('邮箱')).toBeNull()
})
