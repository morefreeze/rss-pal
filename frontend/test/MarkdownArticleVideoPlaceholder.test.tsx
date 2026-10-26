import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import MarkdownArticle from '../src/components/MarkdownArticle'

describe('MarkdownArticle video placeholders', () => {
  it.each([
    ['youtube', 'Ndftcft6XhU', '', 'https://www.youtube.com/watch?v=Ndftcft6XhU'],
    ['youtube', 'LKrwhjOI8Nc', '?start=112', 'https://www.youtube.com/watch?v=LKrwhjOI8Nc&t=112'],
    ['bilibili', 'BV1xx411c7mD', '?page=2&start=42', 'https://www.bilibili.com/video/BV1xx411c7mD?p=2&t=42'],
  ])('restores stored %s placeholders inside newsletter link destinations', (platform, id, params, url) => {
    render(<MarkdownArticle source={`[Newsletter video]([[video:${platform}:${id}${params}]] "Votes: 667 Comments: 148") //youtube [comments→](https://news.ycombinator.com/item?id=50150089)`} />)

    const link = screen.getByRole('link', { name: 'Newsletter video' })
    expect(link.getAttribute('href')).toBe(url)
    expect(link.getAttribute('title')).toBe('Votes: 667 Comments: 148')
    expect(screen.getByRole('link', { name: 'comments→' }).getAttribute('href')).toBe('https://news.ycombinator.com/item?id=50150089')
    expect(screen.queryByTitle(`${platform} video ${id}`)).toBeNull()
  })

  it('renders escaped standalone YouTube placeholders as embeds', () => {
    render(<MarkdownArticle source={'Before\n\n\\[\\[video:youtube:8ONFvAtboZ4\\]\\]\n\nAfter'} />)

    expect(screen.getByTitle('youtube video 8ONFvAtboZ4')).toBeTruthy()
    expect(screen.queryByText(/\[\[video:youtube:8ONFvAtboZ4]]/)).toBeNull()
  })

  it('renders inline YouTube placeholders as links', () => {
    render(
      <MarkdownArticle
        source="([[video:youtube:8ONFvAtboZ4?start=112]]) Grok Bot overview"
      />,
    )

    const link = screen.getByRole('link', { name: '1:52' })
    expect(link.getAttribute('href')).toBe('https://www.youtube.com/watch?v=8ONFvAtboZ4&t=112')
    expect(screen.queryByText(/\[\[video:youtube:8ONFvAtboZ4/)).toBeNull()
  })
})
