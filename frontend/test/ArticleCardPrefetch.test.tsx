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
})
