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

describe('VideoEmbed', () => {
  beforeEach(() => {
    mocks.browserPlayer.mockClear()
  })

  it('loads Bilibili embeds eagerly for WebKit compatibility', () => {
    render(<VideoEmbed platform="bilibili" id="BV1xL3y6cEVv" />)

    const iframe = screen.getByTitle('bilibili video BV1xL3y6cEVv')
    expect(iframe.getAttribute('loading')).toBeNull()
    expect(mocks.browserPlayer).not.toHaveBeenCalled()
  })

  it('routes an inline YouTube video with a start through the browser player', () => {
    render(<VideoEmbed platform="youtube" id="dQw4w9WgXcQ" start={45} />)

    expect(screen.getByTestId('youtube-browser-player')).toBeTruthy()
    expect(mocks.browserPlayer).toHaveBeenCalledOnce()
    expect(mocks.browserPlayer).toHaveBeenCalledWith({
      videoId: 'dQw4w9WgXcQ',
      start: 45,
      originalURL: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=45s',
    })
    expect(screen.queryByTitle('youtube video dQw4w9WgXcQ')).toBeNull()
    expect(screen.queryByRole('link', { name: '在 YouTube 打开' })).toBeNull()
  })

  it.each([
    ['zero', 0],
    ['negative', -5],
    ['not-a-number', Number.NaN],
    ['non-finite', Number.POSITIVE_INFINITY],
  ])('does not add a %s inline start to the original URL', (_, start) => {
    render(<VideoEmbed platform="youtube" id="dQw4w9WgXcQ" start={start} />)

    expect(mocks.browserPlayer).toHaveBeenCalledWith({
      videoId: 'dQw4w9WgXcQ',
      start,
      originalURL: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ',
    })
  })
})
