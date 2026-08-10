import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  browserPlayer: vi.fn(),
}))

vi.mock('../src/components/YouTubeBrowserPlayer', () => ({
  default: (props: {
    videoId: string
    start?: number
    originalURL: string
  }) => {
    mocks.browserPlayer(props)
    return <div data-testid="youtube-browser-player" />
  },
}))

import VideoEmbed from '../src/components/VideoEmbed'

let userAgentSpy: ReturnType<typeof vi.spyOn> | null = null

function setPakeRuntime() {
  Object.defineProperty(window, '__TAURI_IPC__', {
    configurable: true,
    value: vi.fn(),
  })
}

function setPakeUserAgent() {
  userAgentSpy = vi.spyOn(window.navigator, 'userAgent', 'get').mockReturnValue(
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.1 Safari/605.1.15',
  )
}

function clearPakeRuntime() {
  delete (window as Window & { __TAURI_IPC__?: unknown }).__TAURI_IPC__
  userAgentSpy?.mockRestore()
  userAgentSpy = null
}

function expectBilibiliExternalLink() {
  expect(screen.queryByTitle('bilibili video BV1xL3y6cEVv')).toBeNull()
  const link = screen.getByRole('link', { name: '在 B 站打开' })
  expect(link.getAttribute('href')).toBe('https://www.bilibili.com/video/BV1xL3y6cEVv?p=2&t=30')
  expect(link.getAttribute('target')).toBe('_blank')
  expect(link.getAttribute('rel')).toBe('noopener noreferrer')
  expect(mocks.browserPlayer).not.toHaveBeenCalled()
}

describe('VideoEmbed', () => {
  beforeEach(() => {
    clearPakeRuntime()
    mocks.browserPlayer.mockClear()
  })

  it('loads Bilibili embeds eagerly for WebKit compatibility', () => {
    render(<VideoEmbed platform="bilibili" id="BV1xL3y6cEVv" />)

    const iframe = screen.getByTitle('bilibili video BV1xL3y6cEVv')
    expect(iframe.getAttribute('loading')).toBeNull()
    expect(mocks.browserPlayer).not.toHaveBeenCalled()
  })

  it('offers an external Bilibili link in the Pake webview', () => {
    setPakeRuntime()

    render(<VideoEmbed platform="bilibili" id="BV1xL3y6cEVv" page={2} start={30} />)

    expectBilibiliExternalLink()
  })

  it('recognizes the installed Pake fixed Safari user agent', () => {
    setPakeUserAgent()

    render(<VideoEmbed platform="bilibili" id="BV1xL3y6cEVv" page={2} start={30} />)

    expectBilibiliExternalLink()
  })

  it('renders an inline YouTube video as the native embed with a start', () => {
    render(<VideoEmbed platform="youtube" id="dQw4w9WgXcQ" start={45} />)

    const iframe = screen.getByTitle('youtube video dQw4w9WgXcQ')
    expect(iframe.getAttribute('src')).toBe('https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0&start=45')
    expect(iframe.getAttribute('allow')).toContain('encrypted-media')
    expect(iframe.getAttribute('allowfullscreen')).not.toBeNull()
    expect(mocks.browserPlayer).not.toHaveBeenCalled()
    expect(screen.queryByTestId('youtube-browser-player')).toBeNull()
  })

  it.each([
    ['zero', 0],
    ['negative', -5],
    ['not-a-number', Number.NaN],
    ['non-finite', Number.POSITIVE_INFINITY],
  ])('normalizes a %s inline start', (_, start) => {
    render(<VideoEmbed platform="youtube" id="dQw4w9WgXcQ" start={start} />)

    const iframe = screen.getByTitle('youtube video dQw4w9WgXcQ')
    expect(iframe.getAttribute('src')).toBe('https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0')
    expect(mocks.browserPlayer).not.toHaveBeenCalled()
  })
})
