import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Article } from '../src/api/client'

vi.mock('../src/components/YouTubeRelayPlayer', () => ({
  default: ({ articleId, originalURL }: { articleId: number; originalURL: string }) => (
    <div>relay:{articleId}:{originalURL}</div>
  ),
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
  it('uses the authenticated relay for a YouTube article', () => {
    render(<ArticlePlayerCard article={videoArticle({
      media_type: 'video/youtube',
      media_url: 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0',
    })} />)

    expect(screen.getByText(
      'relay:2391:https://www.youtube.com/watch?v=dQw4w9WgXcQ',
    )).toBeTruthy()
    expect(screen.queryByTitle('youtube video dQw4w9WgXcQ')).toBeNull()
  })

  it('keeps Bilibili playback client-direct', () => {
    render(<ArticlePlayerCard article={videoArticle({
      url: 'https://www.bilibili.com/video/BV1xL3y6cEVv',
      media_type: 'video/bilibili',
      media_url: 'https://player.bilibili.com/player.html?bvid=BV1xL3y6cEVv',
    })} />)

    expect(screen.getByTitle('bilibili video BV1xL3y6cEVv')).toBeTruthy()
  })
})
