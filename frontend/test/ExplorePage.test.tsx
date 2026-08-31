import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ExplorePage from '../src/pages/ExplorePage'
import Toaster from '../src/components/Toaster'
import type { ExploreArticleListItem, ExploreListResponse } from '../src/api/client'

const api = vi.hoisted(() => ({
  createExploreFeedback: vi.fn(),
  deleteExploreFeedback: vi.fn(),
  getExplore: vi.fn(),
  getExploreSources: vi.fn(),
  recordExploreArticleEvent: vi.fn(),
  replaceExploreInterests: vi.fn(),
  subscribeExploreSource: vi.fn(),
  subscribeExploreSources: vi.fn(),
}))

const infinite = vi.hoisted(() => ({ options: undefined as any }))

vi.mock('../src/api/client', async importOriginal => ({
  ...await importOriginal<typeof import('../src/api/client')>(),
  ...api,
}))

vi.mock('../src/hooks/useInfiniteScrollTrigger', () => ({
  useInfiniteScrollTrigger: (options: any) => { infinite.options = options },
}))

vi.mock('../src/hooks/useBreakpoint', () => ({ useBreakpoint: () => 'desktop' }))

function article(id: number, sourceId = id, topic = '工程'): ExploreArticleListItem {
  return {
    id,
    source_id: sourceId,
    source_title: `Source ${sourceId}`,
    title: `Article ${id}`,
    url: `https://example.test/${id}`,
    excerpt: `Excerpt ${id}`,
    published_at: '2026-08-31T08:00:00Z',
    fetched_at: '2026-08-31T09:00:00Z',
    topic,
    reason: `Reason ${id}`,
    is_subscribed: false,
    thumbnail_url: id === 1 ? 'https://images.test/1.jpg' : undefined,
  }
}

function response(overrides: Partial<ExploreListResponse> = {}): ExploreListResponse {
  return {
    snapshot: {
      id: 12,
      slot_at: '2026-08-31T12:00:00Z',
      completed_at: '2026-08-31T12:01:00Z',
      generating: false,
      using_fallback: false,
      refresh_failed: false,
      next_refresh_at: '2026-08-31T15:00:00Z',
    },
    articles: [article(1), article(2, 2, '设计')],
    has_more: false,
    ...overrides,
  }
}

function DetailProbe() {
  const location = useLocation()
  const state = location.state as { from?: string; articlePreview?: ExploreArticleListItem } | null
  return <output data-testid="detail-state">{state?.from}|{state?.articlePreview?.id}</output>
}

function renderPage(path = '/explore') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/explore" element={<><ExplorePage /><Toaster /></>} />
        <Route path="/explore/articles/:id" element={<DetailProbe />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('ExplorePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    infinite.options = undefined
    api.getExplore.mockResolvedValue(response())
    api.getExploreSources.mockResolvedValue([])
    api.recordExploreArticleEvent.mockResolvedValue({ recorded: true })
    api.createExploreFeedback.mockResolvedValue({ id: 90 })
    api.deleteExploreFeedback.mockResolvedValue(undefined)
    api.replaceExploreInterests.mockResolvedValue({ interests: [] })
  })

  it('defaults to published descending, switches sort/order/topic, and requests another page', async () => {
    api.getExplore.mockImplementation(({ offset = 0 }: { offset?: number }) => Promise.resolve(
      offset === 0 ? response({ has_more: true }) : response({ articles: [article(3)], has_more: false }),
    ))
    renderPage()
    expect(await screen.findByText('Article 1')).toBeTruthy()
    expect(api.getExplore).toHaveBeenNthCalledWith(1, {
      limit: 20, offset: 0, sort: 'published', order: 'desc',
    })

    fireEvent.click(screen.getByRole('button', { name: /抓取/ }))
    await waitFor(() => expect(api.getExplore).toHaveBeenLastCalledWith(
      expect.objectContaining({ sort: 'captured', order: 'desc', offset: 0 }),
    ))
    fireEvent.click(screen.getByRole('button', { name: /抓取/ }))
    await waitFor(() => expect(api.getExplore).toHaveBeenLastCalledWith(
      expect.objectContaining({ sort: 'captured', order: 'asc', offset: 0 }),
    ))
    fireEvent.change(screen.getByRole('combobox', { name: '主题筛选' }), { target: { value: '工程' } })
    await waitFor(() => expect(api.getExplore).toHaveBeenLastCalledWith(
      expect.objectContaining({ topic: '工程', offset: 0 }),
    ))

    await act(async () => { await infinite.options.onLoadMore() })
    expect(api.getExplore).toHaveBeenLastCalledWith(
      expect.objectContaining({ topic: '工程', offset: 20 }),
    )
    expect(await screen.findByText('Article 3')).toBeTruthy()
  })

  it('shows cold-start, generating, stale fallback, empty filter, and request-failure states', async () => {
    api.getExplore.mockResolvedValueOnce(response({
      snapshot: { ...response().snapshot, id: 0, generating: true },
    }))
    const { unmount } = renderPage()
    expect(await screen.findByText('正在为你发现第一批优质内容')).toBeTruthy()
    expect(screen.getByText('推荐正在后台优化，当前内容可以继续阅读')).toBeTruthy()
    unmount()

    api.getExplore.mockResolvedValueOnce(response({
      snapshot: { ...response().snapshot, using_fallback: true, refresh_failed: true },
    }))
    const fallback = renderPage()
    expect(await screen.findByText(/最近一次更新失败.*沿用/)).toBeTruthy()
    fallback.unmount()

    api.getExplore
      .mockResolvedValueOnce(response())
      .mockResolvedValueOnce(response({ articles: [] }))
    const filtered = renderPage()
    await screen.findByText('Article 1')
    fireEvent.change(screen.getByRole('combobox', { name: '主题筛选' }), { target: { value: '工程' } })
    expect(await screen.findByText('当前主题没有候选文章')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '清除主题筛选' }))
    filtered.unmount()

    api.getExplore.mockRejectedValueOnce(new Error('offline'))
    renderPage()
    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByRole('alert').textContent).toContain('探索内容加载失败')
  })

  it('lets a cold-start user choose fixed interests and retry saving them inline', async () => {
    api.getExplore.mockResolvedValue(response({
      snapshot: { ...response().snapshot, id: 0 },
      articles: [],
    }))
    api.replaceExploreInterests
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ interests: [] })
    renderPage()

    expect(await screen.findByRole('group', { name: '选择探索兴趣' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '编程' }))
    fireEvent.click(screen.getByRole('button', { name: '安全' }))
    fireEvent.click(screen.getByRole('button', { name: '保存兴趣' }))
    expect((await screen.findByRole('alert')).textContent).toContain('兴趣保存失败')
    expect(screen.getByRole('button', { name: '编程' }).getAttribute('aria-pressed')).toBe('true')

    fireEvent.click(screen.getByRole('button', { name: '保存兴趣' }))
    await waitFor(() => expect(api.replaceExploreInterests).toHaveBeenLastCalledWith(['programming', 'security']))
    expect((await screen.findByRole('status')).textContent).toContain('兴趣已保存')
  })

  it('renders exploration metadata and opens a candidate without subscribing', async () => {
    renderPage('/explore?topic=%E5%B7%A5%E7%A8%8B&sort=published&order=desc')
    expect(await screen.findByAltText('Article 1 缩略图')).toBeTruthy()
    expect(screen.getByText('Source 1')).toBeTruthy()
    expect(screen.getByText('Reason 1')).toBeTruthy()
    fireEvent.click(screen.getByRole('article', { name: 'Article 1' }))
    expect(await screen.findByTestId('detail-state')).toHaveProperty('textContent', '/explore?topic=%E5%B7%A5%E7%A8%8B&sort=published&order=desc|1')
    expect(api.recordExploreArticleEvent).toHaveBeenCalledWith(1, 'click')
    expect(api.subscribeExploreSource).not.toHaveBeenCalled()
    expect(api.subscribeExploreSources).not.toHaveBeenCalled()
  })

  it.each(['Enter', ' '])('does not open an article when %j is pressed on its menu button', async key => {
    renderPage()
    await screen.findByText('Article 1')
    const menu = screen.getByRole('button', { name: 'Article 1 的更多操作' })

    fireEvent.keyDown(menu, { key })

    expect(screen.queryByTestId('detail-state')).toBeNull()
    expect(api.recordExploreArticleEvent).not.toHaveBeenCalledWith(1, 'click')
  })

  it('optimistically hides a source and restores its original position from undo', async () => {
    api.getExplore.mockResolvedValue(response({
      articles: [article(1, 7), article(2, 8), article(3, 7)],
    }))
    renderPage()
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByRole('button', { name: 'Article 1 的更多操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '隐藏此源' }))
    await waitFor(() => expect(screen.queryByText('Article 1')).toBeNull())
    expect(screen.queryByText('Article 3')).toBeNull()
    fireEvent.click(await screen.findByRole('button', { name: '撤销' }))
    await waitFor(() => expect(screen.getAllByRole('article').map(node => node.getAttribute('aria-label')))
      .toEqual(['Article 1', 'Article 2', 'Article 3']))
  })

  it('offers a persistent undo entry when feedback empties the unfiltered stream', async () => {
    api.getExplore.mockResolvedValue(response({
      articles: [article(1, 7), article(3, 7)],
    }))
    renderPage()
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByRole('button', { name: 'Article 1 的更多操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '隐藏此源' }))

    const undo = await screen.findByRole('button', { name: '撤销最近反馈' })
    expect(screen.getByText('反馈已隐藏当前候选文章')).toBeTruthy()
    fireEvent.click(undo)

    await waitFor(() => expect(screen.getAllByRole('article').map(node => node.getAttribute('aria-label')))
      .toEqual(['Article 1', 'Article 3']))
  })

  it('clears every feedback created in this page session when several actions empty the stream', async () => {
    api.getExplore.mockResolvedValue(response({
      articles: [article(1, 7), article(2, 8)],
    }))
    api.createExploreFeedback
      .mockResolvedValueOnce({ id: 91 })
      .mockResolvedValueOnce({ id: 92 })
    renderPage()
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByRole('button', { name: 'Article 1 的更多操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '隐藏此源' }))
    await waitFor(() => expect(screen.queryByText('Article 1')).toBeNull())
    fireEvent.click(screen.getByRole('button', { name: 'Article 2 的更多操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '隐藏此源' }))

    fireEvent.click(await screen.findByRole('button', { name: '清除本次反馈（2）' }))

    await waitFor(() => expect(screen.getAllByRole('article').map(node => node.getAttribute('aria-label')))
      .toEqual(['Article 1', 'Article 2']))
    expect(api.deleteExploreFeedback.mock.calls.map(([id]) => id)).toEqual([92, 91])
  })

  it('keeps the empty-state undo available when deleting feedback fails, then allows retry', async () => {
    api.getExplore.mockResolvedValue(response({ articles: [article(1, 7)] }))
    api.deleteExploreFeedback
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(undefined)
    renderPage()
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByRole('button', { name: 'Article 1 的更多操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '隐藏此源' }))

    fireEvent.click(await screen.findByRole('button', { name: '撤销最近反馈' }))
    expect((await screen.findByRole('alert')).textContent).toContain('撤销失败')
    expect(screen.getByRole('button', { name: '撤销最近反馈' })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: '撤销最近反馈' }))
    expect(await screen.findByText('Article 1')).toBeTruthy()
  })

  it('rolls back failed topic dampening and reports the error', async () => {
    api.createExploreFeedback.mockRejectedValue(new Error('offline'))
    renderPage()
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByRole('button', { name: 'Article 1 的更多操作' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '少推荐这类内容' }))
    expect(await screen.findByText('Article 1')).toBeTruthy()
    expect((await screen.findByRole('alert')).textContent).toContain('反馈失败，已恢复')
  })
})
