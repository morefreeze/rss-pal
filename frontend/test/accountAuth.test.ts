import { beforeEach, expect, it, vi } from 'vitest'
const m = vi.hoisted(() => ({
  publicPost: vi.fn(),
  privatePost: vi.fn(),
  get: vi.fn(),
}))
vi.mock('axios', () => ({
  default: { create: () => ({ post: m.publicPost }) },
}))
vi.mock('../src/api/client', () => ({
  api: { post: m.privatePost, get: m.get },
}))
import {
  currentAccountID,
  persistAccountSession,
  startAccountOAuth,
  readAccountFlow,
  safeAccountReturn,
  exchangeAccountTicket,
  verifyEmailCode,
} from '../src/api/accountAuth'
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  sessionStorage.clear()
})
it('restricts return paths and removes stale remembered credentials', () => {
  for (const bad of [
    'https://evil.test',
    '//evil.test',
    '/\\evil.test',
    '/login',
    'javascript:alert(1)',
  ])
    expect(safeAccountReturn(bad)).toBe('/articles')
  expect(safeAccountReturn('/feeds?add=1#part')).toBe('/feeds?add=1#part')
  localStorage.setItem('refresh_token', 'stale')
  localStorage.setItem('refresh_token_expires_at', 'stale')
  persistAccountSession({
    token: 'new',
    user: { id: 9, username: 'reader', is_admin: false },
  })
  expect(currentAccountID()).toBe(9)
  expect(localStorage.getItem('refresh_token')).toBeNull()
  expect(localStorage.getItem('refresh_token_expires_at')).toBeNull()
})
it('sends public verification without the authenticated client or session side effects', async () => {
  localStorage.setItem('token', 'existing')
  localStorage.setItem('user', '{"id":7}')
  m.publicPost.mockRejectedValue({ response: { status: 400 } })
  await expect(
    verifyEmailCode('challenge', '000000', true),
  ).rejects.toBeTruthy()
  expect(m.privatePost).not.toHaveBeenCalled()
  expect(localStorage.getItem('token')).toBe('existing')
})
it('stores only flow context and restricts provider redirects', async () => {
  m.publicPost.mockResolvedValue({
    data: { url: 'https://accounts.google.com/o/oauth2/v2/auth?state=s' },
  })
  await startAccountOAuth('google', '/feeds?add=1', false)
  expect(m.publicPost).toHaveBeenCalledWith('/auth/oauth/google/start', {
    return_to: '/feeds?add=1',
    remember: false,
  })
  expect(readAccountFlow()).toEqual(
    expect.objectContaining({
      owner: null,
      binding: false,
      returnTo: '/feeds?add=1',
    }),
  )
  expect(sessionStorage.getItem('rsspal:account-oauth-flow')).not.toContain(
    'state=',
  )
  m.publicPost.mockResolvedValue({ data: { url: 'https://evil.test/login' } })
  await expect(startAccountOAuth('google', '/articles', true)).rejects.toThrow()
})
it('coalesces replayed ticket requests and never sends binding through public client', async () => {
  m.publicPost.mockResolvedValue({ data: { token: 'app', user: { id: 3 } } })
  const first = exchangeAccountTicket('strict-mode-ticket', false),
    second = exchangeAccountTicket('strict-mode-ticket', false)
  expect(first).toBe(second)
  await first
  expect(m.publicPost).toHaveBeenCalledTimes(1)
  localStorage.setItem('token', 'owner-token')
  localStorage.setItem('user', '{"id":7}')
  m.privatePost.mockResolvedValue({ data: { bound: true } })
  await exchangeAccountTicket('binding-ticket', true)
  expect(m.privatePost).toHaveBeenCalledWith('/auth/oauth/exchange', {
    ticket: 'binding-ticket',
  })
  expect(localStorage.getItem('token')).toBe('owner-token')
})
it('rejects switched-owner or expired flows', async () => {
  let finish!: (v: unknown) => void
  m.privatePost.mockReturnValue(
    new Promise((r) => {
      finish = r
    }),
  )
  localStorage.setItem('token', 'owner-token')
  localStorage.setItem('user', '{"id":7}')
  const pending = startAccountOAuth('google', '/settings#account', true, true)
  localStorage.setItem('user', '{"id":8}')
  finish({ data: { url: 'https://accounts.google.com/auth' } })
  await expect(pending).rejects.toThrow('账号已切换')
  expect(readAccountFlow()).toBeNull()
  sessionStorage.setItem(
    'rsspal:account-oauth-flow',
    JSON.stringify({
      owner: null,
      binding: false,
      returnTo: '/articles',
      createdAt: Date.now() - 600001,
    }),
  )
  expect(readAccountFlow()).toBeNull()
})
