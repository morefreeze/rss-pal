import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import ArticleDetailPreview from '../src/components/ArticleDetailPreview'
import type { ArticleListItem } from '../src/api/client'

describe('ArticleDetailPreview', () => {
  it('shows list metadata without pretending the body is loaded', () => {
    const preview: ArticleListItem = {
      id: 7,
      feed_id: 1,
      feed_title: 'Example Feed',
      title: 'Prefetched title',
      url: 'https://example.com/7',
      published_at: '2026-07-27T00:00:00Z',
      summary_brief: 'Brief from the list',
      fetched_at: '2026-07-27T00:00:00Z',
      manual_tags: [],
    }
    render(<ArticleDetailPreview article={preview} />)
    expect(screen.getByText('Prefetched title')).toBeTruthy()
    expect(screen.getByText(/Example Feed/)).toBeTruthy()
    expect(screen.getByText('Brief from the list')).toBeTruthy()
    expect(screen.getByText('正在加载正文…')).toBeTruthy()
  })
})
