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
  it.each([
    ['image-first blockquote', '> ![](https://example.com/image.png)\n> meaningful quote', 'blockquote', 'article-section-001'],
    ['GFM table', '| Name | Value |\n| --- | --- |\n| A | B |', 'table', 'article-section-001'],
    ['setext heading', 'Setext title\n============', 'h1', 'article-section-001'],
  ])('assigns %s anchors to the parsed block container', (_name, source, selector, id) => {
    const { container } = render(<MarkdownArticle source={source} />)
    expect(container.querySelector(`${selector}#${id}`)).toBeTruthy()
    expect(container.querySelectorAll(`#${id}`)).toHaveLength(1)
  })

  it('assigns IDs directly to blocks without changing article text or real links', () => {
    const article = '    const hidden = true\n\n# Heading\n\nA paragraph with an [external link](https://example.com).\n\n- First item\n\n[rss-pal-anchor](#article-section-002)'
    const { container, rerender } = render(
      <ReaderActionContext.Provider value={readerContext}>
        <MarkdownArticle source={article} />
      </ReaderActionContext.Provider>,
    )

    expect(container.querySelectorAll('#article-section-001, #article-section-002, #article-section-003, #article-section-004')).toHaveLength(4)
    expect(container.querySelector('h1#article-section-001')).toBeTruthy()
    expect(container.querySelector('p#article-section-002')).toBeTruthy()
    expect(container.querySelector('li#article-section-003')).toBeTruthy()
    expect(container.querySelector('pre code')?.textContent).toBe('const hidden = true\n')
    expect(container.querySelectorAll('#article-section-002')).toHaveLength(1)
    expect(screen.getByRole('heading', { name: 'Heading' }).textContent).toBe('Heading')
    expect(screen.getByText('A paragraph with an', { exact: false }).textContent).toBe('A paragraph with an external link.')
    expect(screen.getByRole('listitem').textContent).toBe('First item')
    expect(screen.getByRole('link', { name: 'rss-pal-anchor' }).getAttribute('href')).toBe('#article-section-002')

    const external = screen.getByRole('link', { name: 'external link' })
    expect(external.getAttribute('href')).toBe('https://example.com')
    expect(external.getAttribute('target')).toBe('_blank')
    expect(external.getAttribute('rel')).toBe('noopener noreferrer')

    rerender(
      <ReaderActionContext.Provider value={readerContext}>
        <MarkdownArticle source={article} />
      </ReaderActionContext.Provider>,
    )
    expect([...container.querySelectorAll('[id^="article-section-"]')].map((node) => node.id)).toEqual([
      'article-section-001',
      'article-section-002',
      'article-section-003',
      'article-section-004',
    ])
  })
})
