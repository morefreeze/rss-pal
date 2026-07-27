import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ArticleListPage from '../src/pages/ArticleListPage'
import type { ArticleListItem } from '../src/api/client'

const apiMocks = vi.hoisted(() => ({
  dislikeArticle: vi.fn(),
  getArticles: vi.fn(),
  getFeeds: vi.fn(),
  getGroupedArticles: vi.fn(),
  getRecommended: vi.fn(),
  getTagSidebar: vi.fn(),
  likeArticle: vi.fn(),
  markAllRead: vi.fn(),
  postEvent: vi.fn(async () => undefined),
  searchArticles: vi.fn(),
}))

vi.mock('../src/api/client', () => apiMocks)

const detailCacheMocks = vi.hoisted(() => ({
  prefetchArticleDetail: vi.fn(async () => undefined),
}))

vi.mock('../src/api/articleDetailCache', () => detailCacheMocks)

vi.mock('../src/components/ArticleCard', () => ({
  default: ({
    article,
    prefetchRef,
    onOpen,
    onPrefetch,
  }: {
    article: ArticleListItem
    prefetchRef?: React.RefObject<HTMLDivElement>
    onOpen: (id: number, preview: ArticleListItem) => void
    onPrefetch?: (id: number) => void
  }) => (
    <div
      ref={prefetchRef}
      data-testid={`article-${article.id}`}
      onMouseEnter={() => onPrefetch?.(article.id)}
      onClick={() => onOpen(article.id, article)}
    >
      {article.title}
    </div>
  ),
}))

vi.mock('../src/player/PlayerContext', () => ({
  usePlayer: () => ({ playArticle: vi.fn() }),
}))

vi.mock('../src/hooks/useBreakpoint', () => ({
  useBreakpoint: () => 'desktop',
}))

class TestIntersectionObserver implements IntersectionObserver {
  readonly root: Element | Document | null = null
  readonly rootMargin = '0px'
  readonly thresholds = [0]
  observe: IntersectionObserver['observe'] = vi.fn()
  unobserve: IntersectionObserver['unobserve'] = vi.fn()
  disconnect: IntersectionObserver['disconnect'] = vi.fn()
  takeRecords: IntersectionObserver['takeRecords'] = vi.fn(() => [])
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function makeArticles(startId: number): ArticleListItem[] {
  return Array.from({ length: 20 }, (_, index) => ({
    id: startId + index,
    feed_id: 1,
    title: `Article ${startId + index}`,
    url: `https://example.com/${startId + index}`,
    published_at: '2026-07-27T00:00:00Z',
    summary_brief: '',
    fetched_at: '2026-07-27T00:00:00Z',
    manual_tags: [],
  }))
}

function ArticleLocationProbe() {
  const location = useLocation()
  const state = location.state as {
    articlePreview?: ArticleListItem
  } | null
  return (
    <output data-testid="route-preview">
      {state?.articlePreview?.id ?? 'missing'}
    </output>
  )
}

describe('ArticleListPage automatic pagination', () => {
  let animationFrames: Map<number, FrameRequestCallback>
  let nextFrameId: number
  let targetTop: number

  beforeEach(() => {
    animationFrames = new Map()
    nextFrameId = 1
    targetTop = 10_000
    sessionStorage.clear()
    localStorage.clear()
    vi.clearAllMocks()
    apiMocks.getFeeds.mockResolvedValue([])
    apiMocks.getRecommended.mockResolvedValue([])
    vi.stubGlobal('IntersectionObserver', TestIntersectionObserver)
    vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
      const frameId = nextFrameId++
      animationFrames.set(frameId, callback)
      return frameId
    }))
    vi.stubGlobal('cancelAnimationFrame', vi.fn((frameId: number) => {
      animationFrames.delete(frameId)
    }))
    vi.stubGlobal('innerHeight', 800)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
      () => new DOMRect(0, targetTop, 100, 100),
    )
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  const flushAnimationFrames = () => {
    const queuedFrames = Array.from(animationFrames.values())
    animationFrames.clear()
    queuedFrames.forEach((callback) => callback(performance.now()))
  }

  const flushPromiseChain = async () => {
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
  }

  it('promotes an interacted article into the detail prefetch queue', async () => {
    apiMocks.getArticles.mockImplementation(({ offset }: { offset: number }) =>
      Promise.resolve(offset === 0 ? makeArticles(1) : []),
    )
    render(
      <MemoryRouter initialEntries={['/articles']}>
        <ArticleListPage />
      </MemoryRouter>,
    )
    const first = await screen.findByTestId('article-1')
    fireEvent.mouseEnter(first)
    expect(detailCacheMocks.prefetchArticleDetail).toHaveBeenCalledWith(1)
  })

  it('hands the selected list preview to the article route', async () => {
    apiMocks.getArticles.mockImplementation(({ offset }: { offset: number }) =>
      Promise.resolve(offset === 0 ? makeArticles(1) : []),
    )
    render(
      <MemoryRouter initialEntries={['/articles']}>
        <Routes>
          <Route path="/articles" element={<ArticleListPage />} />
          <Route path="/articles/:id" element={<ArticleLocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByTestId('article-1'))
    expect((await screen.findByTestId('route-preview')).textContent).toBe('1')
  })

  it('waits for reset page zero before rearming automatic page two', async () => {
    const pendingReset = deferred<ArticleListItem[]>()
    const initialArticles = makeArticles(1)
    const resetArticles = makeArticles(101)

    apiMocks.getArticles.mockImplementation((params: {
      offset: number
      unread?: boolean
    }) => {
      if (params.offset === 0 && params.unread) return pendingReset.promise
      if (params.offset === 0) return Promise.resolve(initialArticles)
      return Promise.resolve([])
    })

    render(
      <MemoryRouter initialEntries={['/articles']}>
        <ArticleListPage />
      </MemoryRouter>,
    )

    expect(await screen.findByTestId('article-1')).toBeTruthy()
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    targetTop = 900
    fireEvent.click(screen.getByLabelText('仅未读'))
    await waitFor(() => {
      expect(apiMocks.getArticles).toHaveBeenCalledWith(
        expect.objectContaining({ offset: 0, unread: true }),
      )
    })

    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    expect(
      apiMocks.getArticles.mock.calls.filter(([params]) => params.offset === 20),
    ).toHaveLength(0)

    await act(async () => {
      pendingReset.resolve(resetArticles)
      await flushPromiseChain()
    })
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    expect(
      apiMocks.getArticles.mock.calls.filter(([params]) => params.offset === 20),
    ).toEqual([
      [expect.objectContaining({ offset: 20, unread: true })],
    ])
  })

  it('keeps pagination gated when reset page zero rejects', async () => {
    const pendingReset = deferred<ArticleListItem[]>()
    const initialArticles = makeArticles(1)

    apiMocks.getArticles.mockImplementation((params: {
      offset: number
      unread?: boolean
    }) => {
      if (params.offset === 0 && params.unread) return pendingReset.promise
      if (params.offset === 0) return Promise.resolve(initialArticles)
      return Promise.resolve([])
    })

    render(
      <MemoryRouter initialEntries={['/articles']}>
        <ArticleListPage />
      </MemoryRouter>,
    )

    expect(await screen.findByTestId('article-1')).toBeTruthy()
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    targetTop = 900
    fireEvent.click(screen.getByLabelText('仅未读'))
    await waitFor(() => {
      expect(apiMocks.getArticles).toHaveBeenCalledWith(
        expect.objectContaining({ offset: 0, unread: true }),
      )
    })

    await act(async () => {
      pendingReset.reject(new Error('page zero failed'))
      await flushPromiseChain()
    })
    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    expect(
      apiMocks.getArticles.mock.calls.filter(
        ([params]) => params.unread && params.offset > 0,
      ),
    ).toHaveLength(0)
  })

  it('ignores an old page two while a newer filter reset is pending', async () => {
    const pendingOldPageTwo = deferred<ArticleListItem[]>()
    const pendingNewReset = deferred<ArticleListItem[]>()
    const initialArticles = makeArticles(1)
    const oldPageTwo = makeArticles(21)
    const newResetArticles = makeArticles(101)

    apiMocks.getArticles.mockImplementation((params: {
      offset: number
      unread?: boolean
    }) => {
      if (params.offset === 20 && !params.unread) return pendingOldPageTwo.promise
      if (params.offset === 0 && params.unread) return pendingNewReset.promise
      if (params.offset === 0) return Promise.resolve(initialArticles)
      return Promise.resolve([])
    })

    render(
      <MemoryRouter initialEntries={['/articles']}>
        <ArticleListPage />
      </MemoryRouter>,
    )

    expect(await screen.findByTestId('article-1')).toBeTruthy()
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    targetTop = 900
    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })
    expect(apiMocks.getArticles).toHaveBeenCalledWith(
      expect.objectContaining({ offset: 20, unread: undefined }),
    )

    fireEvent.click(screen.getByLabelText('仅未读'))
    await waitFor(() => {
      expect(apiMocks.getArticles).toHaveBeenCalledWith(
        expect.objectContaining({ offset: 0, unread: true }),
      )
    })

    await act(async () => {
      pendingOldPageTwo.resolve(oldPageTwo)
      await flushPromiseChain()
    })

    expect(screen.queryByTestId('article-21')).toBeNull()
    expect(screen.queryByRole('button', { name: '加载更多' })).toBeNull()

    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })
    expect(
      apiMocks.getArticles.mock.calls.filter(
        ([params]) => params.unread && params.offset > 0,
      ),
    ).toHaveLength(0)

    await act(async () => {
      pendingNewReset.resolve(newResetArticles)
      await flushPromiseChain()
    })
    expect(await screen.findByTestId('article-101')).toBeTruthy()
    expect(screen.queryByTestId('article-1')).toBeNull()
    expect(screen.queryByTestId('article-21')).toBeNull()

    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })
    expect(
      apiMocks.getArticles.mock.calls.filter(
        ([params]) => params.unread && params.offset === 20,
      ),
    ).toHaveLength(1)
  })

  it('starts new-generation page two before old page two settles', async () => {
    const pendingOldPageTwo = deferred<ArticleListItem[]>()
    const pendingNewPageTwo = deferred<ArticleListItem[]>()
    const initialArticles = makeArticles(1)
    const oldPageTwo = makeArticles(21)
    const newResetArticles = makeArticles(101)
    const newPageTwo = makeArticles(121)

    apiMocks.getArticles.mockImplementation((params: {
      offset: number
      unread?: boolean
    }) => {
      if (params.offset === 20 && !params.unread) return pendingOldPageTwo.promise
      if (params.offset === 20 && params.unread) return pendingNewPageTwo.promise
      if (params.offset === 0 && params.unread) return Promise.resolve(newResetArticles)
      if (params.offset === 0) return Promise.resolve(initialArticles)
      return Promise.resolve([])
    })

    render(
      <MemoryRouter initialEntries={['/articles']}>
        <ArticleListPage />
      </MemoryRouter>,
    )

    expect(await screen.findByTestId('article-1')).toBeTruthy()
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    targetTop = 900
    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })
    expect(apiMocks.getArticles).toHaveBeenCalledWith(
      expect.objectContaining({ offset: 20, unread: undefined }),
    )

    fireEvent.click(screen.getByLabelText('仅未读'))
    expect(await screen.findByTestId('article-101')).toBeTruthy()
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })
    expect(
      apiMocks.getArticles.mock.calls.filter(
        ([params]) => params.unread && params.offset === 20,
      ),
    ).toHaveLength(1)

    await act(async () => {
      pendingOldPageTwo.resolve(oldPageTwo)
      await flushPromiseChain()
    })
    expect(screen.queryByTestId('article-21')).toBeNull()

    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })
    expect(
      apiMocks.getArticles.mock.calls.filter(
        ([params]) => params.unread && params.offset === 20,
      ),
    ).toHaveLength(1)

    await act(async () => {
      pendingNewPageTwo.resolve(newPageTwo)
      await flushPromiseChain()
    })
    expect(await screen.findByTestId('article-121')).toBeTruthy()
    expect(screen.queryByTestId('article-21')).toBeNull()
  })
})
