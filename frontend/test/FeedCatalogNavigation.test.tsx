import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import MoreSheet from '../src/components/MoreSheet'
function Location() { return <output>{useLocation().pathname}</output> }
it('mobile admin entry navigates to the catalog', () => {
  render(<MemoryRouter><Location /><MoreSheet open isAdmin onClose={vi.fn()} onLogout={vi.fn()} /></MemoryRouter>)
  fireEvent.click(screen.getByRole('button', { name: /后台管理/ }))
  expect(screen.getByText('/admin')).toBeTruthy()
})
it('mobile reader has no admin catalog entry', () => {
  render(<MemoryRouter><MoreSheet open isAdmin={false} onClose={vi.fn()} onLogout={vi.fn()} /></MemoryRouter>)
  expect(screen.queryByText('后台管理')).toBeNull()
})
