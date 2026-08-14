import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import ArticleCard from '../src/components/ArticleCard'
import type { ArticleListItem } from '../src/api/client'

vi.mock('../src/hooks/useExposureTracking', () => ({
  useExposureTracking: () => ({ current: null }),
  reportClick: vi.fn(),
}))

describe('ArticleCard detail prefetch', () => {
  it('promotes pointer, focus, and touch interactions', () => {
    const article: ArticleListItem = {
      id: 9,
      feed_id: 1,
      title: 'Article 9',
      url: 'https://example.com/9',
      published_at: '2026-07-27T00:00:00Z',
      summary_brief: '',
      fetched_at: '2026-07-27T00:00:00Z',
      manual_tags: [],
    }
    const onPrefetch = vi.fn()
    render(
      <ArticleCard
        article={article}
        isRead={false}
        isFocused={false}
        idx={0}
        onPlay={vi.fn()}
        formatDate={() => ''}
        stripMarkdown={text => text}
        onOpen={vi.fn()}
        onFocus={vi.fn()}
        onPrefetch={onPrefetch}
      />,
    )
    const card = screen.getByText('Article 9').closest('[data-article-card]')!
    fireEvent.pointerEnter(card)
    fireEvent.focus(card)
    fireEvent.touchStart(card)
    expect(onPrefetch.mock.calls).toEqual([[9], [9], [9]])
  })

  it('filters by source without opening the article card', () => {
    const article: ArticleListItem = {
      id: 9,
      feed_id: 3,
      feed_title: 'Source Feed',
      title: 'Article 9',
      url: 'https://example.com/9',
      published_at: '2026-07-27T00:00:00Z',
      summary_brief: '',
      fetched_at: '2026-07-27T00:00:00Z',
      manual_tags: [],
    }
    const onOpen = vi.fn()
    const onSourceFilter = vi.fn()
    sessionStorage.clear()

    render(
      <ArticleCard
        article={article}
        isRead={false}
        isFocused={false}
        idx={0}
        onPlay={vi.fn()}
        formatDate={() => ''}
        stripMarkdown={text => text}
        onOpen={onOpen}
        onFocus={vi.fn()}
        onSourceFilter={onSourceFilter}
        sourceSearch="?saved=1"
      />,
    )

    const source = screen.getByRole('link', { name: '查看 Source Feed 的文章' })
    expect(source.getAttribute('href')).toBe('/articles?saved=1&feed_id=3')
    fireEvent.click(source)
    expect(onSourceFilter).toHaveBeenCalledWith(3, '/articles?saved=1&feed_id=3')
    expect(onOpen).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('selectedFeed')).toBe('3')
  })
})
