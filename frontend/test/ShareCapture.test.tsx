import { StrictMode } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ShareCapturePage from '../src/pages/ShareCapturePage'
import { authSearch, parseAuthIntent, postAuthURL } from '../src/utils/authIntent'

const mocks = vi.hoisted(() => ({
  isLoggedIn: vi.fn(), createOneoffLinkSet: vi.fn(),
}))
vi.mock('../src/api/client', () => mocks)

function subject(search = '?url=https%3A%2F%2Fexample.com%2Farticle%3Fq%3Dhello%26x%3D2') {
  render(<StrictMode><MemoryRouter initialEntries={[`/capture${search}`]}><ShareCapturePage /></MemoryRouter></StrictMode>)
}

beforeEach(() => { vi.resetAllMocks(); mocks.isLoggedIn.mockReturnValue(true) })

describe('share capture handoff', () => {
  it('keeps long encoded CJK URLs through authentication', () => {
    const source = `https://example.com/${'文'.repeat(260)}`
    expect(postAuthURL(parseAuthIntent(`?intent=capture&source=${encodeURIComponent(source)}`)))
      .toBe(`/capture?url=${encodeURIComponent(source)}`)
  })
  it('preserves a capture URL through login and registration without arbitrary redirects', () => {
    const source = 'https://example.com/文章?q=a&x=2#part'
    const intent = parseAuthIntent(`?intent=capture&source=${encodeURIComponent(source)}&next=https://evil.example`)
    expect(intent.kind).toBe('capture')
    expect(postAuthURL(intent)).toBe(`/capture?url=${encodeURIComponent(source)}`)
    expect(parseAuthIntent(authSearch(intent))).toEqual(intent)
    for (const value of ['javascript:alert(1)', 'https://user:pass@example.com', 'http://127.0.0.1', 'http://localhost']) {
      expect(parseAuthIntent(`?intent=capture&source=${encodeURIComponent(value)}`).kind).toBe('use')
    }
  })

  it('requires a user action, submits once while busy, and shows the saved article', async () => {
    let done!: (result: { parent_article_id: number; feed_id: number }) => void
    mocks.createOneoffLinkSet.mockReturnValue(new Promise(resolve => { done = resolve }))
    subject()
    expect(mocks.createOneoffLinkSet).not.toHaveBeenCalled()
    const submit = screen.getByRole('button', { name: '加入抓取' })
    fireEvent.click(submit)
    fireEvent.click(submit)
    expect(mocks.createOneoffLinkSet).toHaveBeenCalledTimes(1)
    expect(mocks.createOneoffLinkSet).toHaveBeenCalledWith('https://example.com/article?q=hello&x=2', false)
    done({ parent_article_id: 42, feed_id: 7 })
    await waitFor(() => expect(screen.getByRole('link', { name: '查看文章' }).getAttribute('href')).toBe('/articles/42'))
  })

  it('retains the URL for login', () => {
    mocks.isLoggedIn.mockReturnValue(false)
    subject('?url=https%3A%2F%2Fexample.com%2Farticle')
    expect(screen.getByRole('link', { name: '登录后继续' }).getAttribute('href')).toBe('/login?intent=capture&source=https%3A%2F%2Fexample.com%2Farticle')
    expect(mocks.createOneoffLinkSet).not.toHaveBeenCalled()
  })

  it.each(['', '?url=javascript%3Aalert(1)', '?url=https://example.com&url=https://other.example', '?url=%zz'])('rejects unusable shared input %s', search => {
    subject(search)
    expect(screen.getByRole('alert').textContent).toContain('有效的网页链接')
    expect(screen.queryByRole('button', { name: '加入抓取' })).toBeNull()
    expect(mocks.createOneoffLinkSet).not.toHaveBeenCalled()
  })

  it('shows a retry after a fetch failure without losing the shared link', async () => {
    mocks.createOneoffLinkSet.mockRejectedValue(new Error('network'))
    subject()
    fireEvent.click(screen.getByRole('button', { name: '加入抓取' }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('抓取失败'))
    expect(screen.getByRole('button', { name: '重试抓取' })).toBeTruthy()
    expect(screen.getByText('https://example.com/article?q=hello&x=2')).toBeTruthy()
  })
})
