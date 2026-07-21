import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import ReadingLayout from '../src/components/ReadingLayout'
import type { ReaderActionContextValue } from '../src/reader/types'

describe('ReadingLayout reader link actions', () => {
  it('uses the same partial-link Selection menu in immersive mode', () => {
    const readerActionContext: ReaderActionContextValue = {
      normalizeLink: (href) => new URL(href, 'https://article.example/post').href,
      getLinkState: () => null,
      getActions: () => [{ id: 'add', label: '加入待抓取（1）', run: vi.fn() }],
    }
    render(
      <ReadingLayout
        article={{
          title: 'Article',
          url: 'https://article.example/post',
          published_at: null,
          word_count: 10,
          reading_minutes: 1,
          content: 'Before [Immersive readable link](https://example.com/page) after',
          summary_brief: '',
          summary_detailed: '',
        }}
        fontSize={18}
        fontFamily="sans"
        codeWrap={false}
        onExit={vi.fn()}
        onFontSize={vi.fn()}
        onFontFamily={vi.fn()}
        onCodeWrap={vi.fn()}
        readerActionContext={readerActionContext}
      />,
    )
    const anchor = screen.getByRole('link', { name: 'Immersive readable link' })
    const range = document.createRange()
    range.setStart(anchor.firstChild!, 2)
    range.setEnd(anchor.firstChild!, 8)
    const selection = window.getSelection()!
    selection.removeAllRanges()
    selection.addRange(range)
    fireEvent.pointerUp(anchor, { pointerType: 'mouse' })
    expect(screen.getByRole('menuitem', { name: '加入待抓取（1）' })).toBeTruthy()
  })
})
