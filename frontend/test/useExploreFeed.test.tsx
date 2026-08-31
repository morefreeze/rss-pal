import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useExploreFeed } from '../src/hooks/useExploreFeed'
import type { ExploreArticleListItem, ExploreListResponse } from '../src/api/client'

const api = vi.hoisted(() => ({
  createExploreFeedback: vi.fn(),
  deleteExploreFeedback: vi.fn(),
  getExplore: vi.fn(),
  recordExploreArticleEvent: vi.fn(),
}))

vi.mock('../src/api/client', async importOriginal => ({
  ...await importOriginal<typeof import('../src/api/client')>(),
  ...api,
}))

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
  }
}

function response(articles: ExploreArticleListItem[], hasMore = false): ExploreListResponse {
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
    articles,
    has_more: hasMore,
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('useExploreFeed', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    sessionStorage.clear()
    localStorage.clear()
    localStorage.setItem('user', JSON.stringify({ id: 11, username: 'alice', is_admin: false }))
    api.getExplore.mockResolvedValue(response([]))
    api.recordExploreArticleEvent.mockResolvedValue({ recorded: true })
  })

  it('starts at published descending and merges later pages by article id', async () => {
    api.getExplore
      .mockResolvedValueOnce(response([article(1), article(2)], true))
      .mockResolvedValueOnce(response([article(2), article(3)]))

    const { result } = renderHook(() => useExploreFeed({ pageSize: 2 }))
    await waitFor(() => expect(result.current.articles.map(item => item.id)).toEqual([1, 2]))
    expect(api.getExplore).toHaveBeenNthCalledWith(1, {
      limit: 2,
      offset: 0,
      sort: 'published',
      order: 'desc',
    })

    await act(async () => { await result.current.loadMore() })
    expect(api.getExplore).toHaveBeenNthCalledWith(2, {
      limit: 2,
      offset: 2,
      sort: 'published',
      order: 'desc',
    })
    expect(result.current.articles.map(item => item.id)).toEqual([1, 2, 3])
  })

  it('deduplicates repeated article ids inside one incoming page', async () => {
    api.getExplore
      .mockResolvedValueOnce(response([article(1)], true))
      .mockResolvedValueOnce(response([article(2), article(2), article(3)]))

    const { result } = renderHook(() => useExploreFeed({ pageSize: 1 }))
    await waitFor(() => expect(result.current.articles.map(item => item.id)).toEqual([1]))
    await act(async () => { await result.current.loadMore() })

    expect(result.current.articles.map(item => item.id)).toEqual([1, 2, 3])
  })

  it('resets paging and ignores a late response from an older sort generation', async () => {
    const oldRequest = deferred<ExploreListResponse>()
    api.getExplore
      .mockReturnValueOnce(oldRequest.promise)
      .mockResolvedValueOnce(response([article(20)]))

    const { result } = renderHook(() => useExploreFeed())
    act(() => result.current.setSort('captured'))
    await waitFor(() => expect(result.current.articles.map(item => item.id)).toEqual([20]))
    expect(api.getExplore).toHaveBeenNthCalledWith(2, expect.objectContaining({
      offset: 0,
      sort: 'captured',
      order: 'desc',
    }))

    await act(async () => { oldRequest.resolve(response([article(10)])); await oldRequest.promise })
    expect(result.current.articles.map(item => item.id)).toEqual([20])
  })

  it('de-noises repeated exposure events within the mounted feed', async () => {
    api.getExplore.mockResolvedValue(response([article(1)]))
    const { result } = renderHook(() => useExploreFeed())
    await waitFor(() => expect(result.current.articles).toHaveLength(1))

    await act(async () => {
      await Promise.all([result.current.recordExposure(1), result.current.recordExposure(1)])
    })
    expect(api.recordExploreArticleEvent).toHaveBeenCalledTimes(1)
    expect(api.recordExploreArticleEvent).toHaveBeenCalledWith(1, 'exposure')
  })

  it('keeps exposure de-noising when the page remounts in the same browser session', async () => {
    api.getExplore.mockResolvedValue(response([article(4)]))
    const first = renderHook(() => useExploreFeed())
    await waitFor(() => expect(first.result.current.articles).toHaveLength(1))
    await act(async () => { await first.result.current.recordExposure(4) })
    first.unmount()

    const second = renderHook(() => useExploreFeed())
    await waitFor(() => expect(second.result.current.articles).toHaveLength(1))
    await act(async () => { await second.result.current.recordExposure(4) })
    expect(api.recordExploreArticleEvent).toHaveBeenCalledTimes(1)
  })

  it('isolates persisted exposure de-noising between signed-in users', async () => {
    api.getExplore.mockResolvedValue(response([article(4)]))
    const first = renderHook(() => useExploreFeed())
    await waitFor(() => expect(first.result.current.articles).toHaveLength(1))
    await act(async () => { await first.result.current.recordExposure(4) })
    first.unmount()

    localStorage.setItem('user', JSON.stringify({ id: 12, username: 'bob', is_admin: false }))
    const second = renderHook(() => useExploreFeed())
    await waitFor(() => expect(second.result.current.articles).toHaveLength(1))
    await act(async () => { await second.result.current.recordExposure(4) })

    expect(api.recordExploreArticleEvent).toHaveBeenCalledTimes(2)
  })

  it('persists an exposure only after the API accepts it and allows retry after failure', async () => {
    api.getExplore.mockResolvedValue(response([article(4)]))
    api.recordExploreArticleEvent
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ recorded: true })
    const { result } = renderHook(() => useExploreFeed())
    await waitFor(() => expect(result.current.articles).toHaveLength(1))

    await act(async () => { await result.current.recordExposure(4) })
    expect(sessionStorage.getItem('exploreReportedExposures:11')).toBeNull()
    await act(async () => { await result.current.recordExposure(4) })

    expect(api.recordExploreArticleEvent).toHaveBeenCalledTimes(2)
    expect(sessionStorage.getItem('exploreReportedExposures:11')).toBe('[4]')
  })

  it('coalesces concurrent exposure requests for the same article', async () => {
    const exposure = deferred<{ recorded: boolean }>()
    api.getExplore.mockResolvedValue(response([article(4)]))
    api.recordExploreArticleEvent.mockReturnValue(exposure.promise)
    const { result } = renderHook(() => useExploreFeed())
    await waitFor(() => expect(result.current.articles).toHaveLength(1))

    let first!: Promise<void>
    let second!: Promise<void>
    act(() => {
      first = result.current.recordExposure(4)
      second = result.current.recordExposure(4)
    })
    expect(api.recordExploreArticleEvent).toHaveBeenCalledTimes(1)
    exposure.resolve({ recorded: true })
    await act(async () => { await Promise.all([first, second]) })
  })

  it('rolls back failed hide feedback without changing original ordering', async () => {
    api.getExplore.mockResolvedValue(response([article(1, 7), article(2, 8), article(3, 7)]))
    api.createExploreFeedback.mockRejectedValue(new Error('offline'))
    const { result } = renderHook(() => useExploreFeed())
    await waitFor(() => expect(result.current.articles).toHaveLength(3))

    await expect(act(async () => { await result.current.hideSource(7) })).rejects.toThrow('offline')
    expect(result.current.articles.map(item => item.id)).toEqual([1, 2, 3])
  })

  it('undoes source hiding into the original positions', async () => {
    api.getExplore.mockResolvedValue(response([article(1, 7), article(2, 8), article(3, 7)]))
    api.createExploreFeedback.mockResolvedValue({ id: 91 })
    api.deleteExploreFeedback.mockResolvedValue(undefined)
    const { result } = renderHook(() => useExploreFeed())
    await waitFor(() => expect(result.current.articles).toHaveLength(3))

    let undo!: () => Promise<void>
    await act(async () => { undo = await result.current.hideSource(7) })
    expect(result.current.articles.map(item => item.id)).toEqual([2])
    await act(async () => { await undo() })
    expect(api.deleteExploreFeedback).toHaveBeenCalledWith(91)
    expect(result.current.articles.map(item => item.id)).toEqual([1, 2, 3])
  })

  it('dampens every article in a topic and restores them on undo', async () => {
    api.getExplore.mockResolvedValue(response([
      article(1, 7, '工程'), article(2, 8, '设计'), article(3, 9, '工程'),
    ]))
    api.createExploreFeedback.mockResolvedValue({ id: 92 })
    api.deleteExploreFeedback.mockResolvedValue(undefined)
    const { result } = renderHook(() => useExploreFeed())
    await waitFor(() => expect(result.current.articles).toHaveLength(3))

    let undo!: () => Promise<void>
    await act(async () => { undo = await result.current.dampenTopic('工程') })
    expect(result.current.articles.map(item => item.id)).toEqual([2])
    expect(api.createExploreFeedback).toHaveBeenCalledWith({
      feedback_type: 'dampen_topic', topic: '工程',
    })
    await act(async () => { await undo() })
    expect(result.current.articles.map(item => item.id)).toEqual([1, 2, 3])
  })

  it('automatically loads another page when local feedback hides every visible article', async () => {
    api.getExplore
      .mockResolvedValueOnce(response([article(1, 7)], true))
      .mockResolvedValueOnce(response([article(2, 8)]))
    api.createExploreFeedback.mockResolvedValue({ id: 93 })
    const { result } = renderHook(() => useExploreFeed({ pageSize: 20 }))
    await waitFor(() => expect(result.current.articles.map(item => item.id)).toEqual([1]))

    await act(async () => { await result.current.hideSource(7) })

    await waitFor(() => expect(api.getExplore).toHaveBeenNthCalledWith(2, expect.objectContaining({ offset: 20 })))
    await waitFor(() => expect(result.current.articles.map(item => item.id)).toEqual([2]))
  })

  it('stops automatic empty-page loading after a bounded number of pages', async () => {
    api.getExplore.mockResolvedValue(response([], true))
    renderHook(() => useExploreFeed({ pageSize: 20 }))

    await waitFor(() => expect(api.getExplore).toHaveBeenCalledTimes(6))
    await new Promise(resolve => setTimeout(resolve, 20))
    expect(api.getExplore).toHaveBeenCalledTimes(6)
  })

  it('does not retry an automatically loaded page after it fails', async () => {
    api.getExplore
      .mockResolvedValueOnce(response([], true))
      .mockRejectedValueOnce(new Error('offline'))
    const { result } = renderHook(() => useExploreFeed({ pageSize: 20 }))

    await waitFor(() => expect(result.current.error).toContain('探索内容加载失败'))
    await new Promise(resolve => setTimeout(resolve, 20))
    expect(api.getExplore).toHaveBeenCalledTimes(2)
  })
})
