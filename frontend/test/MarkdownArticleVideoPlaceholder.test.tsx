import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import MarkdownArticle from '../src/components/MarkdownArticle'

describe('MarkdownArticle video placeholders', () => {
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
