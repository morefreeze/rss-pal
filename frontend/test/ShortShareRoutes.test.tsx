// @vitest-environment-options { "url": "https://r.morefreeze.top/" }
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import App, {
  AppRoutes,
  SHORT_SHARE_HOSTNAME,
  ShortShareRoutes,
  isShortShareHostname,
} from '../src/App'

const axiosMock = vi.hoisted(() => Object.assign(vi.fn(), {
  create: vi.fn(() => ({
    interceptors: {
      request: { use: vi.fn() },
      response: { use: vi.fn() },
    },
  })),
  get: vi.fn(),
}))
const privateAPIMocks = vi.hoisted(() => ({
  isLoggedIn: vi.fn(() => false),
  getUser: vi.fn(),
  getMe: vi.fn(),
}))

vi.mock('axios', () => ({ default: axiosMock }))
vi.mock('../src/api/client', async () => ({
  ...(await vi.importActual<typeof import('../src/api/client')>('../src/api/client')),
  ...privateAPIMocks,
}))
vi.mock('../src/pages/LoginPage', () => ({ default: () => <div>main-site login</div> }))

function sharedSnapshot() {
  return {
    title: 'Short-domain article',
    url: 'https://source.example/post',
    summary_brief: 'Short summary',
    content: 'Public body',
    snapshotted_at: '2026-09-13T00:00:00Z',
  }
}

beforeEach(() => {
  window.history.replaceState({}, '', '/')
  axiosMock.get.mockReset()
  privateAPIMocks.isLoggedIn.mockClear()
  privateAPIMocks.getUser.mockReset()
  privateAPIMocks.getMe.mockReset()
})

describe('short-share hostname routing', () => {
  it('recognizes only the configured hostname after lowercasing and removing one trailing dot', () => {
    expect(SHORT_SHARE_HOSTNAME).toBe('r.morefreeze.top')
    expect(isShortShareHostname('r.morefreeze.top')).toBe(true)
    expect(isShortShareHostname('R.MOREFREEZE.TOP.')).toBe(true)
    expect(isShortShareHostname('r.morefreeze.top..')).toBe(false)
    expect(isShortShareHostname('rss.morefreeze.top')).toBe(false)
  })

  it('renders a valid root short code without hydrating private application state', async () => {
    window.history.replaceState({}, '', '/Aa0000000000?utm_source=ignored#section')
    axiosMock.get.mockResolvedValue({ data: sharedSnapshot() })

    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Short-domain article' })).toBeTruthy()
    expect(axiosMock.get).toHaveBeenCalledWith('/api/s/Aa0000000000', {
      signal: expect.any(AbortSignal),
    })
    expect(privateAPIMocks.isLoggedIn).not.toHaveBeenCalled()
    expect(privateAPIMocks.getUser).not.toHaveBeenCalled()
    expect(privateAPIMocks.getMe).not.toHaveBeenCalled()

    const useRSSPal = screen.getByRole('link', { name: '使用 RSS Pal' })
    expect(useRSSPal.getAttribute('href')).toBe(
      'https://rss.morefreeze.top/login?intent=use&return_to=%2Farticles',
    )
    const shareToX = screen.getByRole('link', { name: '分享到 X' })
    const post = new URL(shareToX.getAttribute('href')!).searchParams.get('text')
    expect(post).toContain('https://r.morefreeze.top/Aa0000000000')
    expect(post).not.toContain('utm_source')
    expect(post).not.toContain('section')
  })

  it.each([
    '/',
    `/${'A'.repeat(11)}`,
    `/${'A'.repeat(13)}`,
    '/Aa00000_0000',
    '/Aa0000000000/',
    '/Aa0000000000//',
    '/Aa0000000000//extra',
    '/Aa0000000000/extra',
    '/share/Aa0000000000',
    '/login',
    '/articles',
    '/api/health',
  ])('rejects every short-domain path outside one exact 12-character code: %s', async path => {
    window.history.replaceState({}, '', path)
    axiosMock.get.mockResolvedValue({ data: sharedSnapshot() })

    render(<App />)

    expect(await screen.findByText('分享链接无效或已过期')).toBeTruthy()
    expect(axiosMock.get).not.toHaveBeenCalled()
    expect(privateAPIMocks.isLoggedIn).not.toHaveBeenCalled()
    expect(privateAPIMocks.getUser).not.toHaveBeenCalled()
    expect(privateAPIMocks.getMe).not.toHaveBeenCalled()
  })

  it('does not expose the root short-code route through the main-site route table', async () => {
    render(
      <MemoryRouter initialEntries={['/Aa0000000000']}>
        <AppRoutes user={null} onLogin={() => {}} onLogout={() => {}} />
      </MemoryRouter>,
    )

    expect(await screen.findByText('main-site login')).toBeTruthy()
    expect(screen.queryByText('Short-domain article')).toBeNull()
    expect(axiosMock.get).not.toHaveBeenCalled()
  })

  it('exposes the short-domain route table independently of App', async () => {
    axiosMock.get.mockResolvedValue({ data: sharedSnapshot() })
    render(
      <MemoryRouter initialEntries={['/Bb1111111111']}>
        <ShortShareRoutes />
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { name: 'Short-domain article' })).toBeTruthy()
    expect(axiosMock.get).toHaveBeenCalledWith('/api/s/Bb1111111111', {
      signal: expect.any(AbortSignal),
    })
  })

  it.each([
    '/Aa0000000000/',
    '/Aa0000000000//',
    '/Aa0000000000//extra',
  ])('rejects a non-exact raw pathname under MemoryRouter: %s', async path => {
    axiosMock.get.mockResolvedValue({ data: sharedSnapshot() })
    render(
      <MemoryRouter initialEntries={[path]}>
        <ShortShareRoutes />
      </MemoryRouter>,
    )

    expect(await screen.findByText('分享链接无效或已过期')).toBeTruthy()
    expect(axiosMock.get).not.toHaveBeenCalled()
    expect(privateAPIMocks.getMe).not.toHaveBeenCalled()
  })
})
