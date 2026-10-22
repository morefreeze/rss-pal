import { StrictMode } from 'react'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import AccountAuthComplete from '../src/pages/AccountAuthComplete'
const m = vi.hoisted(() => ({
  flow: vi.fn(),
  owner: vi.fn(),
  exchange: vi.fn(),
  persist: vi.fn(),
  clear: vi.fn(),
}))
vi.mock('../src/api/accountAuth', () => ({
  readAccountFlow: m.flow,
  currentAccountID: m.owner,
  exchangeAccountTicket: m.exchange,
  persistAccountSession: m.persist,
  clearAccountFlow: m.clear,
  safeAccountReturn: (v: string) => (v.startsWith('/feeds') ? v : '/articles'),
}))
function Destination() {
  const l = useLocation()
  return <output>{l.pathname + l.search}</output>
}
function view() {
  const onLogin = vi.fn()
  render(
    <StrictMode>
      <MemoryRouter initialEntries={['/auth/complete?ticket=one-use-ticket']}>
        <Routes>
          <Route
            path="/auth/complete"
            element={<AccountAuthComplete onLogin={onLogin} />}
          />
          <Route path="*" element={<Destination />} />
        </Routes>
      </MemoryRouter>
    </StrictMode>,
  )
  return onLogin
}
beforeEach(() => {
  vi.clearAllMocks()
  m.owner.mockReturnValue(null)
  m.flow.mockReturnValue({
    owner: null,
    binding: false,
    returnTo: '/feeds?add=1',
    createdAt: Date.now(),
  })
  m.exchange.mockResolvedValue({
    token: 'app-token',
    user: { id: 9, username: 'new', is_admin: false },
    return_to: '/feeds?add=1',
  })
})
it('finishes login and retains the post-login subscription destination', async () => {
  const login = view()
  expect(await screen.findByText('/feeds?add=1')).toBeTruthy()
  expect(login).toHaveBeenCalledWith(expect.objectContaining({ id: 9 }))
  expect(m.persist).toHaveBeenCalledTimes(1)
})
it('rejects a flow after the account changes before exchange', async () => {
  m.owner.mockReturnValue(7)
  view()
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(m.exchange).not.toHaveBeenCalled()
  expect(m.persist).not.toHaveBeenCalled()
})
it('does not replace a newly logged-in account while the exchange is in flight', async () => {
  let finish!: (v: unknown) => void
  m.exchange.mockReturnValue(
    new Promise((r) => {
      finish = r
    }),
  )
  const login = view()
  await waitFor(() => expect(m.exchange).toHaveBeenCalled())
  m.owner.mockReturnValue(7)
  finish({ token: 'old', user: { id: 9, username: 'new', is_admin: false } })
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(login).not.toHaveBeenCalled()
  expect(m.persist).not.toHaveBeenCalled()
})
