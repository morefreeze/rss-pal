import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import SettingsPage from '../src/pages/SettingsPage'

vi.mock('../src/api/client', () => ({
  getTemplates: vi.fn().mockResolvedValue([]),
}))

it('enables summary expansion by default and remembers the setting after reopening settings', async () => {
  localStorage.clear()
  window.location.hash = '#appearance'

  const renderSettings = () => render(<MemoryRouter><SettingsPage /></MemoryRouter>)
  const summaryPreference = () => screen.findByRole<HTMLInputElement>('checkbox', { name: '默认展开 AI 总结' })
  const firstVisit = renderSettings()
  const checkbox = await summaryPreference()
  expect(checkbox.checked).toBe(true)
  fireEvent.click(checkbox)
  expect(checkbox.checked).toBe(false)
  firstVisit.unmount()

  renderSettings()
  expect((await summaryPreference()).checked).toBe(false)
})
