import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import AccountOnboarding from '../src/components/AccountOnboarding'

vi.mock('../src/components/NicknameSetup', () => ({
  NicknameOnboarding: () => <p>Nickname onboarding</p>,
  AccountSetupModal: ({ title, children }: { title: string; children: React.ReactNode }) => <div role="dialog"><h3>{title}</h3>{children}</div>,
}))
const user = { id: 19, username: 'reader_auto', is_admin: false, needs_nickname: true, needs_password: true }
function Location() { const l = useLocation(); return <output>{l.pathname + l.hash}</output> }
function view(current = user, entry = '/articles') {
  return render(<MemoryRouter initialEntries={[entry]}><Location /><AccountOnboarding user={current} onSaved={vi.fn()} /></MemoryRouter>)
}
beforeEach(() => { sessionStorage.clear() })

it('prioritizes setting a password when both password and nickname are missing', () => {
  view()
  expect(screen.getByRole('dialog').textContent).toContain('设置登录密码')
  expect(screen.queryByText('Nickname onboarding')).toBeNull()
})
it('opens account settings without following up with a nickname prompt in the same login', () => {
  const { rerender } = view()
  fireEvent.click(screen.getByRole('button', { name: '去设置密码' }))
  expect(screen.getByText('/settings#account')).toBeTruthy()
  expect(screen.queryByRole('dialog')).toBeNull()
  rerender(<MemoryRouter><AccountOnboarding user={{ ...user, needs_password: false }} onSaved={vi.fn()} /></MemoryRouter>)
  expect(screen.queryByText('Nickname onboarding')).toBeNull()
})
it('suppresses repeated password prompts during a session while keeping accounts independent', () => {
  const { unmount } = view()
  fireEvent.click(screen.getByRole('button', { name: '稍后设置' }))
  expect(screen.queryByRole('dialog')).toBeNull()
  unmount()
  const next = view()
  expect(screen.queryByRole('dialog')).toBeNull()
  next.unmount()
  view({ ...user, id: 20 })
  expect(screen.getByRole('dialog')).toBeTruthy()
})
it('lets users work in account settings without an extra prompt', () => {
  view(user, '/settings#account')
  expect(screen.queryByRole('dialog')).toBeNull()
})
it('keeps password priority after first setup begins directly on account settings', () => {
  const { unmount } = view(user, '/settings#account')
  unmount()
  view({ ...user, needs_password: false })
  expect(screen.queryByText('Nickname onboarding')).toBeNull()
})
