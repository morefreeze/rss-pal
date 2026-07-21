import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import MarkdownArticle from '../src/components/MarkdownArticle'
import { ReaderActionContext } from '../src/reader/ReaderActionContext'
import type { ReaderActionContextValue } from '../src/reader/types'

function readerContext(onDiscovered = vi.fn()): ReaderActionContextValue {
  return {
    normalizeLink: (href) => {
      try {
        const url = new URL(href, 'https://article.example/post')
        return /^https?:$/.test(url.protocol) ? url.href : null
      } catch {
        return null
      }
    },
    getLinkState: (url) => url.includes('draft') ? 'draft' : url.includes('fetched') ? 'fetched' : null,
    getActions: (target) => [{
      id: 'capture',
      label: `处理链接（${target.links.length}）`,
      run: vi.fn(),
    }],
    onLinkDiscovered: onDiscovered,
  }
}

function selectText(node: Node, start: number, end: number) {
  const range = document.createRange()
  range.setStart(node, start)
  range.setEnd(node, end)
  const selection = window.getSelection()!
  selection.removeAllRanges()
  selection.addRange(range)
}

describe('MarkdownArticle reader link actions', () => {
  it('renders link state without mailbox buttons and reports full titles', async () => {
    const discovered = vi.fn()
    render(
      <ReaderActionContext.Provider value={readerContext(discovered)}>
        <MarkdownArticle source="[Draft readable title](/draft) and [Fetched title](/fetched)" />
      </ReaderActionContext.Provider>,
    )
    const draft = screen.getByRole('link', { name: 'Draft readable title' })
    const fetched = screen.getByRole('link', { name: 'Fetched title' })
    expect(draft.classList.contains('reader-link-draft')).toBe(true)
    expect(draft.getAttribute('data-reader-link-state')).toBe('draft')
    expect(fetched.getAttribute('data-reader-link-state')).toBe('fetched')
    expect(screen.queryByRole('button', { name: /标记|抓取/ })).toBeNull()
    await waitFor(() => {
      expect(discovered).toHaveBeenCalledWith({
        url: 'https://article.example/draft',
        title: 'Draft readable title',
      })
    })
  })

  it('opens the configured action when only part of an HTTP link is selected', () => {
    render(
      <ReaderActionContext.Provider value={readerContext()}>
        <MarkdownArticle source="Before [Readable Link](https://example.com/page) after" />
      </ReaderActionContext.Provider>,
    )
    const anchor = screen.getByRole('link', { name: 'Readable Link' })
    selectText(anchor.firstChild!, 3, 7)
    fireEvent.pointerUp(anchor, { pointerType: 'mouse' })
    expect(screen.getByRole('menuitem', { name: '处理链接（1）' })).toBeTruthy()
    expect(anchor.classList.contains('reader-context-target')).toBe(true)
  })
})
