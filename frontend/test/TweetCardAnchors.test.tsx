import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { Article, ArticleListItem } from '../src/api/client'
import TweetCard from '../src/components/TweetCard'

const article: Article = {
  id: 1,
  feed_id: 1,
  title: 'A tweet',
  url: 'https://x.com/alice/status/1',
  content: '> @alice (Alice) · 2026-08-24\n\nFirst paragraph\n\nSecond paragraph',
  published_at: '2026-08-24T00:00:00Z',
  summary_brief: '',
  summary_detailed: '',
  fetched_at: '2026-08-24T00:00:00Z',
  manual_tags: [],
  kind: 'tweet',
}

function compactArticle(id: number): ArticleListItem {
  return {
    id,
    feed_id: 1,
    title: `Tweet ${id}`,
    url: `https://x.com/alice/status/${id}`,
    published_at: '2026-08-24T00:00:00Z',
    summary_brief: 'Compact summary',
    fetched_at: '2026-08-24T00:00:00Z',
    manual_tags: [],
    kind: 'tweet',
  }
}

describe('TweetCard article anchors', () => {
  it('keeps backend numbering after extracting the visible byline', () => {
    const { container } = render(<TweetCard article={article} />)

    expect(screen.getByText('Alice')).toBeTruthy()
    expect(screen.getByText('@alice')).toBeTruthy()
    expect(screen.getByText('· 2026-08-24')).toBeTruthy()
    expect(container.querySelector('.tweet-card-body blockquote')).toBeNull()
    expect(container.querySelector('p#article-section-002')?.textContent).toBe('First paragraph')
    expect(container.querySelector('p#article-section-003')?.textContent).toBe('Second paragraph')
    expect(container.querySelector('header#article-section-001')?.classList.contains('article-section-anchor')).toBe(true)
    expect(container.querySelectorAll('#article-section-001')).toHaveLength(1)
    expect(container.querySelectorAll('#article-section-002')).toHaveLength(1)
    expect(container.querySelectorAll('#article-section-003')).toHaveLength(1)
  })

  it('does not install article anchors in repeated compact cards', () => {
    const { container } = render(
      <>
        <TweetCard article={compactArticle(1)} compact />
        <TweetCard article={compactArticle(2)} compact />
      </>,
    )

    expect(screen.getAllByText('Compact summary')).toHaveLength(2)
    expect(container.querySelectorAll('[id^="article-section-"]')).toHaveLength(0)
  })
})
