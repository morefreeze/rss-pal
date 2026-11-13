import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useNavigate } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import SiteTraffic from '../src/components/SiteTraffic'

function Navigation() {
  const navigate = useNavigate()
  return <>
    <button onClick={() => navigate('/articles/42?token=private')}>Article</button>
    <button onClick={() => navigate('/articles/42?sort=recent')}>Filter</button>
    <button onClick={() => navigate('/admin/monitoring')}>Admin</button>
  </>
}

it('counts route views with one anonymous visitor and no URL payload', async () => {
  localStorage.removeItem('rsspal.traffic.visitor')
  localStorage.setItem('token', 'local-test-session')
  const fetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
  vi.stubGlobal('fetch', fetch)
  try {
    render(<MemoryRouter initialEntries={['/register?utm_source=private']}><SiteTraffic /><Navigation /></MemoryRouter>)
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByText('Article'))
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
    fireEvent.click(screen.getByText('Filter'))
    fireEvent.click(screen.getByText('Admin'))
    const events = fetch.mock.calls.map(([url, options]) => {
      expect(url).toBe('/api/traffic/page-view')
      expect(options.credentials).toBe('omit')
      return JSON.parse(options.body)
    })
    expect(events[0]).toEqual({ event: expect.any(String), visitor: expect.any(String), source: 'campaign' })
    expect(events[1]).toEqual({ event: expect.any(String), visitor: events[0].visitor, source: 'internal' })
    expect(events[1].event).not.toBe(events[0].event)
  } finally {
    localStorage.removeItem('token')
    vi.unstubAllGlobals()
  }
})

it('excludes private route arrivals when the visitor has no session', async () => {
  localStorage.removeItem('token')
  const fetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
  vi.stubGlobal('fetch', fetch)
  try {
    render(<MemoryRouter initialEntries={['/articles/42']}><SiteTraffic /></MemoryRouter>)
    expect(fetch).not.toHaveBeenCalled()
  } finally {
    vi.unstubAllGlobals()
  }
})
