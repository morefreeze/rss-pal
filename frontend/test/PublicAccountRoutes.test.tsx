import type { ReactNode } from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, it, expect, vi } from 'vitest'
import LoginPage from '../src/pages/LoginPage'
import RegisterPage from '../src/pages/RegisterPage'
import AccountLoginMethods from '../src/components/AccountLoginMethods'
const m = vi.hoisted(() => ({
  methods: vi.fn(),
  identities: vi.fn(),
  owner: vi.fn(),
  login: vi.fn(),
}))
vi.mock('../src/api/accountAuth', () => ({
  getAuthMethods: m.methods,
  getAccountIdentities: m.identities,
  currentAccountID: m.owner,
}))
vi.mock('../src/api/client', () => ({
  getRegistrationConfig: vi.fn(async () => ({ available: false })),
  register: vi.fn(),
  login: m.login,
  api: {
    post: vi.fn(async () => {
      throw Error('initialized')
    }),
  },
}))
vi.mock('../src/components/VerifiedAccountAccess', () => ({
  default: ({ mode, onBound, children, primaryForm }: { mode: string; onBound?: () => void; children?: ReactNode; primaryForm?: ReactNode }) => (
    <div>
      verified {mode}
      {primaryForm}
      {children}
      {onBound && <button onClick={onBound}>fixture bound</button>}
    </div>
  ),
}))
beforeEach(() => {
  vi.clearAllMocks()
  m.owner.mockReturnValue(7)
  m.methods.mockResolvedValue({
    public_registration: true,
    legacy_password: true,
    email: true,
    google: true,
    apple: false,
  })
  m.identities.mockResolvedValue({
    identities: [{ provider: 'google', email: 'reader@example.com' }],
  })
})
it('public signup removes invite/password fields and preserves subscription intent', async () => {
  render(
    <MemoryRouter
      initialEntries={[
        '/register?intent=subscribe&source=https%3A%2F%2Fsource.example%2Frss',
      ]}
    >
      <RegisterPage onLogin={vi.fn()} />
    </MemoryRouter>,
  )
  expect(await screen.findByText('verified register')).toBeTruthy()
  expect(screen.queryByPlaceholderText('邀请码')).toBeNull()
  expect(screen.queryByPlaceholderText('密码（至少 6 位）')).toBeNull()
  expect(
    screen.getByRole('link', { name: '已有账号？登录' }).getAttribute('href'),
  ).toContain('intent=subscribe')
})
it('keeps public registration visible and legacy password login accessible', async () => {
  render(
    <MemoryRouter>
      <LoginPage onLogin={vi.fn()} />
    </MemoryRouter>,
  )
  expect(await screen.findByText('verified login')).toBeTruthy()
  expect(screen.getByRole('link', { name: '注册账号' })).toBeTruthy()
  expect(screen.queryByPlaceholderText('密码')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '使用账号密码登录' }))
  expect(screen.getByPlaceholderText('密码')).toBeTruthy()
})
it('shows the bound identity and reloads only after successful binding', async () => {
  render(<AccountLoginMethods />)
  expect(
    await screen.findByText('Google：已绑定（reader@example.com）'),
  ).toBeTruthy()
  expect(m.identities).toHaveBeenCalledTimes(1)
  fireEvent.click(screen.getByRole('button', { name: 'fixture bound' }))
  await waitFor(() => expect(m.identities).toHaveBeenCalledTimes(2))
})
