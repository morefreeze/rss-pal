import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import MarkdownArticle from '../src/components/MarkdownArticle'
import { ReaderActionContext } from '../src/reader/ReaderActionContext'
import type { ReaderActionContextValue } from '../src/reader/types'

const readerContext: ReaderActionContextValue = {
  normalizeLink: (href) => href,
  getLinkState: () => null,
  getActions: () => [],
  onLinkDiscovered: () => {},
}

describe('MarkdownArticle article anchors', () => {
  it('renders invisible block-local targets without changing article text or real links', () => {
    const article = '# Heading\n\nA paragraph with an [external link](https://example.com).\n\n- First item'
    const { container, rerender } = render(
      <ReaderActionContext.Provider value={readerContext}>
        <MarkdownArticle source={article} />
      </ReaderActionContext.Provider>,
    )

    expect(container.querySelectorAll('.article-section-anchor')).toHaveLength(3)
    expect(container.querySelector('h1 > #article-section-001')).toBeTruthy()
    expect(container.querySelector('p > #article-section-002')).toBeTruthy()
    expect(container.querySelector('li > #article-section-003')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Heading' }).textContent).toBe('Heading')
    expect(screen.getByText('A paragraph with an', { exact: false }).textContent).toBe('A paragraph with an external link.')
    expect(screen.getByRole('listitem').textContent).toBe('First item')
    expect(screen.queryByText('rss-pal-anchor')).toBeNull()

    const external = screen.getByRole('link', { name: 'external link' })
    expect(external.getAttribute('href')).toBe('https://example.com')
    expect(external.getAttribute('target')).toBe('_blank')
    expect(external.getAttribute('rel')).toBe('noopener noreferrer')

    rerender(
      <ReaderActionContext.Provider value={readerContext}>
        <MarkdownArticle source={article} />
      </ReaderActionContext.Provider>,
    )
    expect([...container.querySelectorAll('.article-section-anchor')].map((node) => node.id)).toEqual([
      'article-section-001',
      'article-section-002',
      'article-section-003',
    ])
  })
})
