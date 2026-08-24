import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { Article } from '../src/api/client'

import ArticlePlayerCard from '../src/components/ArticlePlayerCard'
import MarkdownArticle, { findStandaloneVideoAnchorID } from '../src/components/MarkdownArticle'
import SummaryMarkdown from '../src/components/SummaryMarkdown'
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

function renderArticleVideo(article: Article, summarySource = '') {
  const primaryVideo = parseStoredEmbedURL(
    article.media_url ?? '',
    article.media_type ?? '',
  )
  const articleAnchorID = primaryVideo
    ? findStandaloneVideoAnchorID(article.content, primaryVideo)
    : undefined
  return render(
    <>
      <ArticlePlayerCard article={article} articleAnchorID={articleAnchorID} />
      {summarySource && <SummaryMarkdown source={summarySource} />}
      <MarkdownArticle
        source={article.content}
        suppressVideo={primaryVideo ?? undefined}
      />
    </>,
  )
}

describe('article video render deduplication', () => {
  it('finds the canonical ID for the matching standalone placeholder', () => {
    expect(findStandaloneVideoAnchorID(
      'Before\n\n[[video:youtube:dQw4w9WgXcQ]]\n\nAfter',
      { platform: 'youtube', id: 'dQw4w9WgXcQ' },
    )).toBe('article-section-002')
  })

  it('moves the suppressed placeholder target onto the visible primary player', () => {
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
      configurable: true,
      value: () => {},
    })
    const { container } = renderArticleVideo(videoArticle({
      media_type: 'video/youtube',
      media_url: 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ',
      content: 'Before\n\n[[video:youtube:dQw4w9WgXcQ]]\n\nAfter',
    }), '[Jump](#article-section-002)')

    const target = container.querySelector('#article-section-002')
    expect(target).toBeTruthy()
    expect(target?.hasAttribute('hidden')).toBe(false)
    expect(target?.classList.contains('article-section-anchor')).toBe(true)
    expect(target?.querySelector('iframe[title="youtube video dQw4w9WgXcQ"]')).toBeTruthy()
    expect(container.querySelectorAll('#article-section-002')).toHaveLength(1)
    expect(container.querySelectorAll('iframe[title="youtube video dQw4w9WgXcQ"]')).toHaveLength(1)

    fireEvent.click(screen.getByRole('link', { name: '跳转原文' }), { detail: 1 })
    expect(target?.classList.contains('article-anchor-highlight')).toBe(true)
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

    expect(screen.getAllByTitle(/youtube video/)).toHaveLength(2)
    expect(screen.getAllByTitle(/youtube video/).map(
      node => node.getAttribute('title'),
    )).toEqual(['youtube video dQw4w9WgXcQ', 'youtube video M7lc1UVf-VE'])
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
