import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ExploreArticlePage from '../src/pages/ExploreArticlePage'
import type { ExploreArticleDetail, ExploreArticleListItem } from '../src/api/client'

const api = vi.hoisted(() => ({
  getExploreArticle: vi.fn(),
  recordExploreArticleEvent: vi.fn(),
  subscribeExploreSource: vi.fn(),
  saveArticle: vi.fn(),
  unsaveArticle: vi.fn(),
  likeArticle: vi.fn(),
  dislikeArticle: vi.fn(),
  updateProgress: vi.fn(),
  resetProgress: vi.fn(),
  shareArticle: vi.fn(),
  generateSummaryStream: vi.fn(),
}))

vi.mock('../src/api/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../src/api/client')>()
  return { ...actual, ...api }
})

const detail: ExploreArticleDetail = {
  id: 23,
  source_id: 7,
  source_title: 'Signal & Noise',
  source_url: 'https://signal.example/feed.xml',
  site_url: 'https://signal.example',
  title: 'A safer candidate reader',
  url: 'https://signal.example/posts/safe-reader',
  content: '# 正文标题\n\n候选文章正文。\n\n```ts\nconst safe = true\n```',
  excerpt: '候选文章摘要',
  published_at: '2026-08-31T08:00:00Z',
  fetched_at: '2026-08-31T09:00:00Z',
  is_subscribed: false,
}

const preview: ExploreArticleListItem = {
  id: 23,
  source_id: 7,
  source_title: 'Signal & Noise',
  title: '列表预览标题',
  url: detail.url,
  excerpt: '候选文章摘要',
  published_at: detail.published_at,
  fetched_at: detail.fetched_at,
  topic: '工程',
  reason: '与你订阅的工程博客相关',
  is_subscribed: false,
}

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{`${location.pathname}${location.search}`}</output>
}

function DetailHarness() {
  const navigate = useNavigate()
  return (
    <>
      <ExploreArticlePage />
      <button type="button" data-testid="switch-detail" onClick={() => navigate('/explore/articles/24')}>
        打开另一篇候选文章
      </button>
    </>
  )
}

function renderPage(entry: string | { pathname: string; state?: unknown } = '/explore/articles/23') {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/explore/articles/:id" element={<DetailHarness />} />
        <Route path="/explore" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  )
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('ExploreArticlePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    document.title = 'RSS Pal'
    api.getExploreArticle.mockResolvedValue(detail)
    api.recordExploreArticleEvent.mockResolvedValue({ recorded: true })
    api.subscribeExploreSource.mockResolvedValue({ feed_id: 70, created: true, copied_articles: 4 })
  })

  it('loads the candidate detail, renders safe Markdown and updates the preview title', async () => {
    const request = deferred<ExploreArticleDetail>()
    api.getExploreArticle.mockReturnValue(request.promise)
    renderPage({
      pathname: '/explore/articles/23',
      state: { from: '/explore?topic=engineering', articlePreview: preview },
    })

    expect(screen.getByRole('status').textContent).toContain('正在加载')
    expect(document.title).toBe('列表预览标题 - RSS Pal')
    request.resolve(detail)

    expect(await screen.findByRole('heading', { name: 'A safer candidate reader' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: '正文标题' })).toBeTruthy()
    expect(screen.getByText('候选文章正文。')).toBeTruthy()
    expect(screen.getByRole('link', { name: '原文链接' }).getAttribute('href')).toBe(detail.url)
    expect(screen.getByRole('link', { name: 'Signal & Noise' }).getAttribute('href')).toBe(detail.site_url)
    await waitFor(() => expect(document.title).toBe('A safer candidate reader - RSS Pal'))
    expect(api.getExploreArticle).toHaveBeenCalledWith(23)
    // The list card already records this transition; the detail must not duplicate it.
    expect(api.recordExploreArticleEvent).not.toHaveBeenCalledWith(23, 'click')
  })

  it('records one click for a direct entry and one completed-read event at the threshold', async () => {
    renderPage()
    await screen.findByRole('heading', { name: 'A safer candidate reader' })
    await waitFor(() => expect(api.recordExploreArticleEvent).toHaveBeenCalledWith(23, 'click'))

    Object.defineProperty(document.documentElement, 'scrollHeight', { configurable: true, value: 2000 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 500 })
    Object.defineProperty(window, 'scrollY', { configurable: true, value: 1400 })
    fireEvent.scroll(window)
    fireEvent.scroll(window)

    await waitFor(() => {
      expect(api.recordExploreArticleEvent.mock.calls.filter(call => call[1] === 'completed_read')).toEqual([
        [23, 'completed_read'],
      ])
    })
  })

  it('returns to the exact Explore list state but rejects unsafe or non-list paths', async () => {
    const valid = renderPage({
      pathname: '/explore/articles/23',
      state: { from: '/explore?topic=%E5%B7%A5%E7%A8%8B&sort=published', articlePreview: preview },
    })
    fireEvent.click(await screen.findByRole('button', { name: '返回探索' }))
    expect(screen.getByTestId('location').textContent).toBe('/explore?topic=%E5%B7%A5%E7%A8%8B&sort=published')
    valid.unmount()

    for (const from of ['https://evil.example/explore', '//evil.example/explore', '/articles', '/explore/articles/99']) {
      const unsafe = renderPage({ pathname: '/explore/articles/23', state: { from } })
      fireEvent.click(await screen.findByRole('button', { name: '返回探索' }))
      expect(screen.getByTestId('location').textContent).toBe('/explore')
      unsafe.unmount()
    }
  })

  it.each([
    [403, '无权访问这篇探索文章'],
    [404, '探索文章不存在或已失效'],
    [500, '探索文章加载失败'],
  ])('shows a recoverable error for HTTP %s', async (status, message) => {
    api.getExploreArticle.mockRejectedValueOnce({ response: { status } })
    renderPage()

    expect((await screen.findByRole('alert')).textContent).toContain(message)
    expect(screen.getByRole('button', { name: '重试' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByRole('heading', { name: 'A safer candidate reader' })).toBeTruthy()
    expect(api.getExploreArticle).toHaveBeenCalledTimes(2)
  })

  it('subscribes only from either explicit button and updates both buttons atomically', async () => {
    const request = deferred<{ feed_id: number; created: boolean; copied_articles: number }>()
    api.subscribeExploreSource.mockReturnValue(request.promise)
    renderPage()
    const buttons = await screen.findAllByRole('button', { name: '订阅此来源' })
    expect(buttons).toHaveLength(2)

    fireEvent.click(buttons[0])
    expect(api.subscribeExploreSource).toHaveBeenCalledTimes(1)
    expect(api.subscribeExploreSource).toHaveBeenCalledWith(7)
    expect(screen.getAllByRole('button', { name: '订阅中…' })).toHaveLength(2)
    request.resolve({ feed_id: 70, created: true, copied_articles: 4 })

    const subscribed = await screen.findAllByRole('button', { name: '已订阅' })
    expect(subscribed).toHaveLength(2)
    expect(subscribed.every(button => (button as HTMLButtonElement).disabled)).toBe(true)
  })

  it('does not apply a stale subscription result after navigating to another candidate', async () => {
    const request = deferred<{ feed_id: number; created: boolean; copied_articles: number }>()
    api.subscribeExploreSource.mockReturnValue(request.promise)
    api.getExploreArticle.mockImplementation(async (id: number) => ({
      ...detail,
      id,
      source_id: id === 23 ? 7 : 8,
      title: id === 23 ? detail.title : 'Second candidate',
      is_subscribed: false,
    }))
    renderPage()

    fireEvent.click((await screen.findAllByRole('button', { name: '订阅此来源' }))[0])
    fireEvent.click(screen.getByTestId('switch-detail'))
    expect(await screen.findByRole('heading', { name: 'Second candidate' })).toBeTruthy()
    request.resolve({ feed_id: 70, created: true, copied_articles: 4 })

    await waitFor(() => expect(screen.getAllByRole('button', { name: '订阅此来源' })).toHaveLength(2))
    expect(screen.queryByRole('button', { name: '已订阅' })).toBeNull()
  })

  it('honours an already-subscribed detail and exposes reader settings without formal actions', async () => {
    localStorage.setItem('rsspal:reader-settings', JSON.stringify({
      fontSize: 18,
      fontFamily: 'serif',
      confettiEnabled: false,
      codeWrap: true,
    }))
    api.getExploreArticle.mockResolvedValue({ ...detail, is_subscribed: true })
    renderPage()

    const article = await screen.findByRole('article', { name: 'A safer candidate reader' })
    expect(article.style.fontSize).toBe('18px')
    expect(article.style.fontFamily).toBe('var(--font-serif)')
    expect(article.querySelector('.code-block-wrap')?.getAttribute('data-wrap')).toBe('true')
    expect(screen.getAllByRole('button', { name: '已订阅' })).toHaveLength(2)

    fireEvent.click(screen.getByRole('button', { name: '阅读设置' }))
    expect(screen.getByText('18 px')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'A+' }))
    expect(article.style.fontSize).toBe('19px')

    for (const label of ['喜欢', '不喜欢', '保存', '标记已读', '标签', '分享到', 'AI 摘要', '阅读进度']) {
      expect(screen.queryByText(label, { exact: false })).toBeNull()
    }
    expect(api.saveArticle).not.toHaveBeenCalled()
    expect(api.unsaveArticle).not.toHaveBeenCalled()
    expect(api.likeArticle).not.toHaveBeenCalled()
    expect(api.dislikeArticle).not.toHaveBeenCalled()
    expect(api.updateProgress).not.toHaveBeenCalled()
    expect(api.resetProgress).not.toHaveBeenCalled()
    expect(api.shareArticle).not.toHaveBeenCalled()
    expect(api.generateSummaryStream).not.toHaveBeenCalled()
    expect(api.subscribeExploreSource).not.toHaveBeenCalled()
  })
})
