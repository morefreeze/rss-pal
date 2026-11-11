import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import DailyPage from '../src/pages/DailyPage'

const api = vi.hoisted(() => ({ getDailyDigest: vi.fn(), setBriefingLastTab: vi.fn() }))
vi.mock('../src/api/client', async original => ({ ...(await original<object>()), ...api }))
vi.mock('../src/components/BriefingCalendar', () => ({ default: () => null }))
const digest = (date: string) => ({
  requested_date: date, shown_date: date, mode: 'cached', pending: false,
  intro_text: `导语 ${date}`, articles: [{ id: 42, title: `文章 ${date}`, feed_title: '来源', published_at: null }],
})
function Result() { const location = useLocation(); return <div data-testid="return-path">{location.state?.from}</div> }
beforeEach(() => { vi.clearAllMocks(); sessionStorage.clear() })
it('carries the displayed fallback issue in the reader link return state', async () => {
  api.getDailyDigest.mockResolvedValue({ ...digest('2026-11-05'), requested_date: '2026-11-06', pending: true })
  render(<MemoryRouter initialEntries={['/daily?date=2026-11-06']}><Routes>
    <Route path="/daily" element={<DailyPage />} /><Route path="/articles/:id" element={<Result />} />
  </Routes></MemoryRouter>)
  fireEvent.click(await screen.findByText('文章 2026-11-05'))
  expect(screen.getByTestId('return-path').textContent).toBe('/daily?date=2026-11-05')
})
it('ignores a stale digest after changing the issue URL', async () => {
  let resolveOld!: (value: ReturnType<typeof digest>) => void
  api.getDailyDigest.mockImplementation((date: string) => date === '2026-11-05'
    ? new Promise(resolve => { resolveOld = resolve }) : Promise.resolve(digest(date)))
  function Switch() { const navigate = useNavigate(); return <><button onClick={() => navigate('/daily?date=2026-11-06')}>换期</button><DailyPage /></> }
  render(<MemoryRouter initialEntries={['/daily?date=2026-11-05']}><Switch /></MemoryRouter>)
  fireEvent.click(screen.getByText('换期'))
  await screen.findByText('文章 2026-11-06')
  await act(async () => resolveOld(digest('2026-11-05')))
  await waitFor(() => expect(screen.queryByText('文章 2026-11-05')).toBeNull())
})
