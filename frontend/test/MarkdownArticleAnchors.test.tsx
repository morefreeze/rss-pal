import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

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
  it('keeps same-article TOC jumps inside the current reader and preserves fallback links', () => {
    const url = 'https://simonwillison.net/2026/Oct/20/battledrones/'
    const source = `- [Jump](${url}#v1-the-initial-prototype)
- [Missing](${url}#missing)
- [Other](https://other.example/post#v1-the-initial-prototype)

#### v1: the *initial* prototype

Body`
    const onLinkDiscovered = vi.fn()
    const { container } = render(
      <ReaderActionContext.Provider value={{ ...readerContext, onLinkDiscovered }}>
        <MarkdownArticle source={source} originalUrl={url} />
      </ReaderActionContext.Provider>,
    )
    const heading = screen.getByRole('heading', { name: 'v1: the initial prototype' })
    const scroll = vi.fn()
    heading.scrollIntoView = scroll
    const link = screen.getByRole('link', { name: 'Jump' })
    expect(link.getAttribute('href')).toBe(`#${heading.id}`)
    expect(link.getAttribute('target')).toBeNull()
    fireEvent.click(link)
    expect(scroll).toHaveBeenCalled()
    expect(screen.getByRole('link', { name: 'Missing' }).getAttribute('href')).toBe(`${url}#missing`)
    expect(screen.getByRole('link', { name: 'Other' }).getAttribute('target')).toBe('_blank')
    expect(onLinkDiscovered.mock.calls.some(([link]) => link.url.startsWith('#'))).toBe(false)
    expect(container.querySelectorAll(`#${heading.id}`)).toHaveLength(1)
  })

  it('resolves encoded fragments and repeated headings in read-only content', () => {
    const url = 'https://example.com/article?version=2'
    const { container } = render(<MarkdownArticle originalUrl={url} readOnly source={`[First](#%E4%B8%AD%E6%96%87-title)

[Second](#中文-title-1)

[Missing](#gone)

[Different query](https://example.com/article?version=3#中文-title)

## 中文 Title

## 中文 Title`} />)
    const headings = [...container.querySelectorAll('h2')]
    expect(screen.getByRole('link', { name: 'First' }).getAttribute('href')).toBe(`#${headings[0].id}`)
    expect(screen.getByRole('link', { name: 'Second' }).getAttribute('href')).toBe(`#${headings[1].id}`)
    expect(screen.getByRole('link', { name: 'Missing' }).getAttribute('href')).toBe(`${url}#gone`)
    expect(screen.getByRole('link', { name: 'Different query' }).getAttribute('href')).toBe('https://example.com/article?version=3#%E4%B8%AD%E6%96%87-title')
  })

  it('resolves reference-style TOCs and scopes clicks to their own reader', () => {
    const source = '[Jump][toc]\n\n[toc]: https://example.com/post#heading\n\n## Heading'
    const first = render(<MarkdownArticle source={source} originalUrl="https://example.com/post" readOnly />)
    const second = render(<MarkdownArticle source={source} originalUrl="https://example.com/post" readOnly />)
    const firstHeading = first.container.querySelector('h2')!
    const secondHeading = second.container.querySelector('h2')!
    const firstScroll = vi.fn()
    const secondScroll = vi.fn()
    firstHeading.scrollIntoView = firstScroll
    secondHeading.scrollIntoView = secondScroll
    expect(second.container.querySelector('a')?.getAttribute('href')).toBe(`#${secondHeading.id}`)
    fireEvent.click(second.container.querySelector('a')!, { button: 0 })
    expect(secondScroll).toHaveBeenCalledOnce()
    expect(firstScroll).not.toHaveBeenCalled()
    expect(first.container.querySelector('a')?.getAttribute('href')).toBe(`#${firstHeading.id}`)
  })

  it('keeps only a precisely shaped public share asset on the same-origin path', () => {
    const token = 'v1_00000000000000000000000000000001_kVCen8vHGjySlZF_mC5N22Bs13LHqfgxBbnHY7Botcg'
    const valid = `/api/share/${token}/assets/3.jpeg`
    const legacy = '/api/share/aB3dE6gH/assets/9.png'
    const short = '/api/s/Aa0000000000/assets/7.jpg'
    const invalid = [
      '/api/share/v1_0123456789abcdef_signature/assets/3.jpeg',
      `/api/s/${'A'.repeat(11)}/assets/1.png`,
      `/api/s/${'A'.repeat(13)}/assets/1.png`,
      '/api/s/Aa00000_0000/assets/1.png',
      '/api/s/Aa0000000000/extra/assets/1.png',
      'https://images.example/path/api/s/Aa0000000000/assets/1.png',
    ]
    const { container } = render(
      <MarkdownArticle
        source={[
          `![shared](${valid})`,
          `![legacy](${legacy})`,
          `![short](${short})`,
          ...invalid.map((url, index) => `![invalid-${index}](${url})`),
        ].join('\n\n')}
        readOnly
      />,
    )

    expect(container.querySelector('img[alt="shared"]')?.getAttribute('src')).toBe(valid)
    expect(container.querySelector('img[alt="legacy"]')?.getAttribute('src')).toBe(legacy)
    expect(container.querySelector('img[alt="short"]')?.getAttribute('src')).toBe(short)
    invalid.forEach((url, index) => {
      expect(container.querySelector(`img[alt="invalid-${index}"]`)?.getAttribute('src')).toBe(
        `/api/proxy/image?url=${encodeURIComponent(url)}`,
      )
    })
  })

  it('keeps authenticated interaction by default but disables it in read-only mode', () => {
    const source = '[Alpha readable link](https://example.com/a)'
    const onLinkDiscovered = vi.fn()
    const view = render(
      <ReaderActionContext.Provider value={{
        ...readerContext,
        getActions: () => [{ id: 'add', label: '加入待抓取', run: () => {} }],
        onLinkDiscovered,
      }}>
        <MarkdownArticle source={source} />
      </ReaderActionContext.Provider>,
    )
    const anchor = screen.getByRole('link', { name: 'Alpha readable link' })
    const range = document.createRange()
    range.selectNodeContents(anchor)
    window.getSelection()?.removeAllRanges()
    window.getSelection()?.addRange(range)
    fireEvent.pointerUp(anchor, { pointerType: 'mouse', button: 0 })
    expect(screen.getByRole('menu')).toBeTruthy()
    expect(onLinkDiscovered).toHaveBeenCalled()

    onLinkDiscovered.mockReset()
    view.rerender(
      <ReaderActionContext.Provider value={{
        ...readerContext,
        getActions: () => [{ id: 'add', label: '加入待抓取', run: () => {} }],
        onLinkDiscovered,
      }}>
        <MarkdownArticle source={source} readOnly />
      </ReaderActionContext.Provider>,
    )
    window.getSelection()?.removeAllRanges()
    const readOnlyAnchor = screen.getByRole('link', { name: 'Alpha readable link' })
    const readOnlyRange = document.createRange()
    readOnlyRange.selectNodeContents(readOnlyAnchor)
    window.getSelection()?.addRange(readOnlyRange)
    fireEvent.pointerUp(readOnlyAnchor, { pointerType: 'mouse', button: 0 })
    expect(screen.queryByRole('menu')).toBeNull()
    expect(onLinkDiscovered).not.toHaveBeenCalled()
  })

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

  it('keeps image-alt cleanup and scanner numbering in lockstep', () => {
    const source = 'Intro\n\n![multi-line alt\n\ntext](https://example.com/a.png)\n\nAfter'
    const { container } = render(<MarkdownArticle source={source} />)

    expect(container.querySelector('p#article-section-001')?.textContent).toBe('Intro')
    expect(container.querySelector('p#article-section-002')?.textContent).toBe('After')
    expect(container.querySelectorAll('[id^="article-section-"]')).toHaveLength(2)
  })

  it('keeps the canonical target on a rendered standalone video placeholder', () => {
    const source = 'Before\n\n[[video:youtube:dQw4w9WgXcQ]]\n\nAfter'
    const { container } = render(<MarkdownArticle source={source} />)

    const target = container.querySelector('#article-section-002')
    expect(target?.classList.contains('article-section-anchor')).toBe(true)
    expect(target?.querySelector('iframe[title="youtube video dQw4w9WgXcQ"]')).toBeTruthy()
    expect(container.querySelectorAll('iframe[title="youtube video dQw4w9WgXcQ"]')).toHaveLength(1)
  })

  it('does not leave a duplicate body target when the standalone video is suppressed', () => {
    const source = 'Before\n\n[[video:youtube:dQw4w9WgXcQ]]\n\nAfter'
    const { container } = render(
      <MarkdownArticle
        source={source}
        suppressVideo={{ platform: 'youtube', id: 'dQw4w9WgXcQ' }}
      />,
    )

    expect(container.querySelector('#article-section-002')).toBeNull()
    expect(container.querySelectorAll('iframe[title="youtube video dQw4w9WgXcQ"]')).toHaveLength(0)
    expect(container.querySelector('p#article-section-003')?.textContent).toBe('After')
  })

  it('assigns three unique IDs to a four-space nested list', () => {
    const source = '- parent\n    - nested\n- sibling'
    const { container } = render(<MarkdownArticle source={source} />)

    expect([...container.querySelectorAll('li[id^="article-section-"]')].map((node) => node.id)).toEqual([
      'article-section-001',
      'article-section-002',
      'article-section-003',
    ])
    expect(container.querySelector('li#article-section-001 li#article-section-002')?.textContent).toBe('nested')
  })

  it('does not anchor a top-level four-space indented list marker parsed as code', () => {
    const source = '    - top-level code\n\nText'
    const { container } = render(<MarkdownArticle source={source} />)

    expect(container.querySelector('pre code')?.textContent).toBe('- top-level code\n')
    expect(container.querySelector('p#article-section-001')?.textContent).toBe('Text')
    expect(container.querySelectorAll('[id^="article-section-"]')).toHaveLength(1)
  })
})
