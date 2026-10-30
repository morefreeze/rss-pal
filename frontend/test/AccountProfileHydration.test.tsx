import { act, render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { MainSiteApp, type User } from '../src/App'

const mocks = vi.hoisted(() => ({ me: vi.fn() }))
vi.mock('../src/api/client', async importOriginal => ({
  ...await importOriginal<typeof import('../src/api/client')>(),
  getMe: mocks.me,
}))
vi.mock('../src/components/Layout', () => ({ default: ({ user }: { user: User }) => <p>{user.needs_password ? 'Password missing' : 'Password set'}</p> }))
vi.mock('../src/components/AccountOnboarding', () => ({ default: ({ user }: { user: User }) => <p>{user.needs_password ? 'Password guidance' : user.needs_nickname ? 'Nickname guidance' : 'Setup complete'}</p> }))
vi.mock('../src/hooks/useConnectionKeepAlive', () => ({ useConnectionKeepAlive: () => {} }))
const user = { id: 19, username: 'reader_auto', is_admin: false, needs_password: true }
beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('token', 'test-token')
  localStorage.setItem('user', JSON.stringify(user))
  window.history.replaceState({}, '', '/settings#account')
})
it('does not overwrite a newer persisted password state with delayed boot profile data', async () => {
  let finish!: (user: User) => void
  mocks.me.mockReturnValue(new Promise<User>(resolve => { finish = resolve }))
  render(<MainSiteApp />)
  expect(screen.getByText('Password missing')).toBeTruthy()
  const updated = { ...user, needs_password: false }
  localStorage.setItem('user', JSON.stringify(updated))
  await act(async () => { finish(user) })
  expect(JSON.parse(localStorage.getItem('user')!).needs_password).toBe(false)
})

it('waits for fresh onboarding eligibility before showing a nickname prompt from a stale cache', async () => {
  localStorage.setItem('user', JSON.stringify({ ...user, needs_password: undefined, needs_nickname: true }))
  let finish!: (user: User) => void
  mocks.me.mockReturnValue(new Promise<User>(resolve => { finish = resolve }))
  render(<MainSiteApp />)
  expect(screen.queryByText('Nickname guidance')).toBeNull()
  await act(async () => { finish({ ...user, needs_nickname: true }) })
  expect(screen.getByText('Password guidance')).toBeTruthy()
  expect(screen.queryByText('Nickname guidance')).toBeNull()
})

it('does not repeat nickname guidance that another device has acknowledged', async () => {
  localStorage.setItem('user', JSON.stringify({ ...user, needs_password: false, needs_nickname: true }))
  let finish!: (user: User) => void
  mocks.me.mockReturnValue(new Promise<User>(resolve => { finish = resolve }))
  render(<MainSiteApp />)
  expect(screen.queryByText('Nickname guidance')).toBeNull()
  await act(async () => { finish({ ...user, needs_password: false, needs_nickname: false }) })
  expect(screen.queryByText('Nickname guidance')).toBeNull()
  expect(screen.getByText('Setup complete')).toBeTruthy()
})
