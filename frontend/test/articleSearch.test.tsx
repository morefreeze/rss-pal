import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import MarkdownArticle from '../src/components/MarkdownArticle'
import SummaryMarkdown from '../src/components/SummaryMarkdown'
import { ArticleSearch, ExcludeArticleSearch, SearchText } from '../src/components/ArticleSearch'
import { findSearchMatches } from '../src/utils/searchMatches'

describe('search text ranges', () => {
  it('treats punctuation literally and preserves original character offsets', () => {
    expect(findSearchMatches('中 A+B / a+b 文', 'a+b')).toEqual([{ start: 2, end: 5 }, { start: 8, end: 11 }])
    expect(findSearchMatches('中文', ' ')).toEqual([])
    expect(findSearchMatches('😀搜索搜索', '搜索')).toEqual([{ start: 2, end: 4 }, { start: 4, end: 6 }])
  })
  it('maps pinyin and initials to Chinese characters', () => {
    expect(findSearchMatches('科技周刊', 'keji')).toEqual([{ start: 0, end: 2 }])
    expect(findSearchMatches('科技周刊', 'kj')).toEqual([{ start: 0, end: 2 }])
  })
})

describe('article search', () => {
  it('counts title, summaries and inline-split body matches, wraps, and closes safely', async () => {
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    const { container } = render(<ArticleSearch query="search" articleKey="1">
      <h1><SearchText text="Search title" /></h1>
      <SummaryMarkdown source="A SEARCH summary" />
      <MarkdownArticle source={'A se**ar**ch paragraph.\n\n`search` code and [search](https://example.com).'} />
    </ArticleSearch>)
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('1/5'))
    expect(container.querySelectorAll('mark').length).toBe(7)
    fireEvent.click(screen.getByRole('button', { name: '上一个匹配' }))
    expect(screen.getByRole('status').textContent).toBe('5/5')
    fireEvent.click(screen.getByRole('button', { name: '下一个匹配' }))
    expect(screen.getByRole('status').textContent).toBe('1/5')
    expect(scroll).toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '退出搜索' }))
    await waitFor(() => expect(container.querySelectorAll('mark').length).toBe(0))
    expect(screen.getByRole('link', { name: 'search' }).getAttribute('href')).toBe('https://example.com')
  })
  it('keeps video placeholders rendered as media during search', async () => {
    const { container } = render(<ArticleSearch query="youtube" articleKey="video"><MarkdownArticle source="[[video:youtube:dQw4w9WgXcQ]]" /></ArticleSearch>)
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('0/0'))
    expect(container.textContent).not.toContain('[[video:')
    expect(container.querySelector('iframe')).not.toBeNull()
  })
  it('does not join separate paragraphs into one false match', async () => {
    render(<ArticleSearch query="boundary" articleKey="blocks"><MarkdownArticle source={'bound\n\nary'} /></ArticleSearch>)
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('0/0'))
  })
  it('excludes linked child article summaries from the current article count', async () => {
    const { container } = render(<ArticleSearch query="search" articleKey="parent">
      <SearchText text="Search parent" />
      <ExcludeArticleSearch><SummaryMarkdown source="Search in a different article" /></ExcludeArticleSearch>
    </ArticleSearch>)
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('1/1'))
    expect(container.querySelectorAll('mark').length).toBe(1)
  })
  it('reports zero honestly and resets when navigating to a new article', async () => {
    const { rerender } = render(<ArticleSearch query="needle" articleKey="1"><SearchText text="nothing" /></ArticleSearch>)
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('0/0'))
    expect(screen.getByRole('button', { name: '下一个匹配' }).hasAttribute('disabled')).toBe(true)
    rerender(<ArticleSearch query="needle" articleKey="2"><SearchText text="needle needle" /></ArticleSearch>)
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('1/2'))
  })
})
