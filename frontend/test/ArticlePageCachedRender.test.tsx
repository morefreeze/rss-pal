import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ArticlePage from '../src/pages/ArticlePage'
import {
  putArticleDetail,
  resetArticleDetailCache,
} from '../src/api/articleDetailCache'
import type { ArticleDetailResponse, ArticleListItem } from '../src/api/client'

const apiMocks = vi.hoisted(() => ({
  dislikeArticle: vi.fn(async () => undefined),
  expandLinkSetChild: vi.fn(async () => undefined),
  exportMarkdown: vi.fn(async () => ''),
  fetchContent: vi.fn(async () => ({ content: '' })),
  generateSummaryStream: vi.fn(),
  getArticle: vi.fn(),
  getTemplates: vi.fn(async () => []),
  hideArticle: vi.fn(async () => undefined),
  likeArticle: vi.fn(async () => undefined),
  recordReadDuration: vi.fn(async () => undefined),
  resetProgress: vi.fn(async () => undefined),
  saveArticle: vi.fn(async () => undefined),
  shareArticle: vi.fn(async () => ({ token: 'token' })),
  unhideArticle: vi.fn(async () => undefined),
  unsaveArticle: vi.fn(async () => undefined),
  updateProgress: vi.fn(async () => ({
    article_id: 42,
    scroll_position: 0,
    is_completed: false,
  })),
}))

vi.mock('../src/api/client', () => apiMocks)

vi.mock('../src/components/MarkdownArticle', () => ({
  default: ({ source }: { source: string }) => (
    <div data-testid="article-body">{source}</div>
  ),
}))
vi.mock('../src/components/TagBar', () => ({ default: () => null }))
vi.mock('../src/components/ArticlePlayerCard', () => ({ default: () => null }))
vi.mock('../src/components/ArticleActionsMenu', () => ({ default: () => null }))
vi.mock('../src/components/ArticleProgressBar', () => ({ default: () => null }))
vi.mock('../src/components/BatchFetchConfirmDialog', () => ({
  BatchFetchConfirmDialog: () => null,
}))
vi.mock('../src/components/LinkSetChildren', () => ({
  LinkSetChildren: () => null,
}))
vi.mock('../src/components/CollapsibleFab', () => ({ default: () => null }))
vi.mock('../src/components/BackFab', () => ({ default: () => null }))
vi.mock('../src/components/BackToTopButton', () => ({ default: () => null }))
vi.mock('../src/components/ReadingLayout', () => ({ default: () => null }))
vi.mock('../src/hooks/useReaderSettings', () => ({
  useReaderSettings: () => ({
    mode: 'normal',
    fontSize: 17,
    fontFamily: 'serif',
    codeWrap: false,
    setMode: vi.fn(),
    setFontSize: vi.fn(),
    setFontFamily: vi.fn(),
    setCodeWrap: vi.fn(),
    toggleMode: vi.fn(),
  }),
}))
vi.mock('../src/hooks/useReadingChrome', () => ({
  useReadingChrome: () => ({ toggle: vi.fn() }),
}))
vi.mock('../src/utils/articleNav', () => ({
  readNavList: () => [],
  readNavContext: () => null,
  writeNav: vi.fn(),
  fetchMoreIds: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(resolvePromise => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function detail(id: number): ArticleDetailResponse {
  return {
    article: {
      id,
      feed_id: 1,
      feed_title: 'Cached Feed',
      title: 'Cached title',
      url: `https://example.com/${id}`,
      content: 'Cached body',
      published_at: '2026-07-27T00:00:00Z',
      summary_brief: 'Cached brief',
      summary_detailed: '',
      fetched_at: '2026-07-27T00:00:00Z',
      manual_tags: [],
    },
    signals: {},
  }
}

class TestResizeObserver implements ResizeObserver {
  observe = vi.fn()
  unobserve = vi.fn()
  disconnect = vi.fn()
  constructor(_callback: ResizeObserverCallback) {}
}

describe('ArticlePage immediate loading', () => {
  beforeEach(() => {
    resetArticleDetailCache()
    vi.clearAllMocks()
    vi.stubGlobal('ResizeObserver', TestResizeObserver)
  })

  it('renders cached full content while revalidation is pending', () => {
    const fresh = deferred<ArticleDetailResponse>()
    apiMocks.getArticle.mockReturnValue(fresh.promise)
    putArticleDetail(detail(42))

    render(
      <MemoryRouter initialEntries={['/articles/42']}>
        <Routes>
          <Route path="/articles/:id" element={<ArticlePage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getByTestId('article-body').textContent).toBe('Cached body')
    expect(apiMocks.getArticle).toHaveBeenCalledWith(42)
  })

  it('renders a matching list preview while a cold request is pending', () => {
    const fresh = deferred<ArticleDetailResponse>()
    apiMocks.getArticle.mockReturnValue(fresh.promise)
    const articlePreview: ArticleListItem = {
      id: 42,
      feed_id: 1,
      feed_title: 'Preview Feed',
      title: 'Preview title',
      url: 'https://example.com/42',
      published_at: '2026-07-27T00:00:00Z',
      summary_brief: 'Preview brief',
      fetched_at: '2026-07-27T00:00:00Z',
      manual_tags: [],
    }

    render(
      <MemoryRouter initialEntries={[{
        pathname: '/articles/42',
        state: { from: '/articles', articlePreview },
      }]}>
        <Routes>
          <Route path="/articles/:id" element={<ArticlePage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getByText('Preview title')).toBeTruthy()
    expect(screen.getByText('Preview brief')).toBeTruthy()
  })
})
