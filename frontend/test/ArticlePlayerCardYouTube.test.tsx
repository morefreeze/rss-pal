import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Article } from '../src/api/client'

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

import ArticlePlayerCard from '../src/components/ArticlePlayerCard'

function videoArticle(overrides: Partial<Article>): Article {
  return {
    id: 2391,
    feed_id: 1,
    title: 'video',
    url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ',
    content: '',
    published_at: null,
    summary_brief: '',
    summary_detailed: '',
    fetched_at: '2026-07-30T00:00:00Z',
    ...overrides,
  }
}

describe('ArticlePlayerCard YouTube routing', () => {
  beforeEach(() => {
    mocks.browserPlayer.mockClear()
  })

  it('routes a stored YouTube article through the browser player', () => {
    render(<ArticlePlayerCard article={videoArticle({
      media_type: 'video/youtube',
      media_url: 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0',
    })} />)

    expect(screen.getByTestId('youtube-browser-player')).toBeTruthy()
    expect(mocks.browserPlayer).toHaveBeenCalledOnce()
    expect(mocks.browserPlayer).toHaveBeenCalledWith({
      videoId: 'dQw4w9WgXcQ',
      start: undefined,
      originalURL: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ',
    })
    expect(screen.queryByTitle('youtube video dQw4w9WgXcQ')).toBeNull()
  })

  it('preserves a positive stored YouTube start in the browser player URL', () => {
    render(<ArticlePlayerCard article={videoArticle({
      media_type: 'video/youtube',
      media_url: 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=45',
    })} />)

    expect(mocks.browserPlayer).toHaveBeenCalledWith({
      videoId: 'dQw4w9WgXcQ',
      start: 45,
      originalURL: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=45s',
    })
  })

  it.each([
    ['zero', '0', 0],
    ['non-finite', '9'.repeat(400), Number.POSITIVE_INFINITY],
  ])('does not add an invalid %s stored start to the original URL', (_, rawStart, start) => {
    render(<ArticlePlayerCard article={videoArticle({
      media_type: 'video/youtube',
      media_url: `https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=${rawStart}`,
    })} />)

    expect(mocks.browserPlayer).toHaveBeenCalledWith({
      videoId: 'dQw4w9WgXcQ',
      start,
      originalURL: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ',
    })
  })

  it('keeps Bilibili playback client-direct', () => {
    render(<ArticlePlayerCard article={videoArticle({
      url: 'https://www.bilibili.com/video/BV1xL3y6cEVv',
      media_type: 'video/bilibili',
      media_url: 'https://player.bilibili.com/player.html?bvid=BV1xL3y6cEVv',
    })} />)

    expect(screen.getByTitle('bilibili video BV1xL3y6cEVv')).toBeTruthy()
    expect(mocks.browserPlayer).not.toHaveBeenCalled()
  })
})
