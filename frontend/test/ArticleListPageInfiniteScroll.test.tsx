import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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
  searchArticles: vi.fn(),
}))

vi.mock('../src/api/client', () => apiMocks)

vi.mock('../src/components/ArticleCard', () => ({
  default: ({
    article,
    prefetchRef,
  }: {
    article: ArticleListItem
    prefetchRef?: React.RefObject<HTMLDivElement>
  }) => (
    <div ref={prefetchRef} data-testid={`article-${article.id}`}>
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
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
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
})
