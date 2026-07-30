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
    return <div data-testid="youtube-browser-player" data-video-id={props.videoId} />
  },
}))

import ArticlePlayerCard from '../src/components/ArticlePlayerCard'
import MarkdownArticle from '../src/components/MarkdownArticle'
import { parseStoredEmbedURL } from '../src/components/parseVideoPlaceholder'

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

function renderArticleVideo(article: Article) {
  const primaryVideo = parseStoredEmbedURL(
    article.media_url ?? '',
    article.media_type ?? '',
  )
  render(
    <>
      <ArticlePlayerCard article={article} />
      <MarkdownArticle
        source={article.content}
        suppressVideo={primaryVideo ?? undefined}
      />
    </>,
  )
}

describe('article video render deduplication', () => {
  beforeEach(() => {
    mocks.browserPlayer.mockClear()
  })

  it('suppresses only the matching inline YouTube player and preserves surrounding content', () => {
    renderArticleVideo(videoArticle({
      media_type: 'video/youtube',
      media_url: 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ',
      content: [
        'Before',
        '',
        '[[video:youtube:dQw4w9WgXcQ?start=90]]',
        '',
        'Between',
        '',
        '[[video:youtube:M7lc1UVf-VE?start=12]]',
        '',
        'After',
      ].join('\r\n'),
    }))

    expect(screen.getAllByTestId('youtube-browser-player')).toHaveLength(2)
    expect(screen.getAllByTestId('youtube-browser-player').map(
      node => node.getAttribute('data-video-id'),
    )).toEqual(['dQw4w9WgXcQ', 'M7lc1UVf-VE'])
    expect(screen.getByText('Before')).toBeTruthy()
    expect(screen.getByText('Between')).toBeTruthy()
    expect(screen.getByText('After')).toBeTruthy()
  })

  it('keeps the primary and a different inline Bilibili iframe', () => {
    renderArticleVideo(videoArticle({
      url: 'https://www.bilibili.com/video/BV1xL3y6cEVv',
      media_type: 'video/bilibili',
      media_url: 'https://player.bilibili.com/player.html?bvid=BV1xL3y6cEVv',
      content: [
        'Before',
        '',
        '[[video:bilibili:BV1xL3y6cEVv?page=2]]',
        '',
        'Between',
        '',
        '[[video:bilibili:BV1Q541167Qg?start=30]]',
        '',
        'After',
      ].join('\n'),
    }))

    expect(screen.getAllByTitle(/bilibili video/)).toHaveLength(2)
    expect(screen.getAllByTitle('bilibili video BV1xL3y6cEVv')).toHaveLength(1)
    expect(screen.getByTitle('bilibili video BV1Q541167Qg')).toBeTruthy()
    expect(screen.getByText('Before')).toBeTruthy()
    expect(screen.getByText('Between')).toBeTruthy()
    expect(screen.getByText('After')).toBeTruthy()
  })
})
