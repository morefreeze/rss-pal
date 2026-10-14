import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ArticlePage from '../src/pages/ArticlePage'
import {
  peekArticleDetail,
  putArticleDetail,
  resetArticleDetailCache,
} from '../src/api/articleDetailCache'
import type { Article, ArticleDetailResponse, ArticleListItem } from '../src/api/client'

const apiMocks = vi.hoisted(() => ({
  updateArticleNote: vi.fn(async () => ({ capture_note: '新想法' })),
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
  listArticleShares: vi.fn(async () => []),
  createArticleShare: vi.fn(),
  revokeArticleShare: vi.fn(),
  unhideArticle: vi.fn(async () => undefined),
  unsaveArticle: vi.fn(async () => undefined),
  updateProgress: vi.fn(async () => ({
    article_id: 42,
    scroll_position: 0,
    is_completed: false,
  })),
}))

const markdownMocks = vi.hoisted(() => ({
  findVideoAnchor: vi.fn(() => 'article-section-001'),
  render: vi.fn(),
}))

const playerCardMocks = vi.hoisted(() => ({
  render: vi.fn(),
}))

vi.mock('../src/api/client', () => apiMocks)

vi.mock('../src/components/MarkdownArticle', () => ({
  findStandaloneVideoAnchorID: markdownMocks.findVideoAnchor,
  default: (props: {
    source: string
    imageDimensions?: Record<string, [number, number]>
    suppressVideo?: { platform: 'youtube' | 'bilibili'; id: string }
  }) => {
    markdownMocks.render(props)
    return <div data-testid="article-body">{props.source}</div>
  },
}))
vi.mock('../src/components/TagBar', () => ({ default: () => null }))
vi.mock('../src/components/ArticlePlayerCard', () => ({
  default: (props: { article: Article; articleAnchorID?: string }) => {
    playerCardMocks.render(props)
    return null
  },
}))
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
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    vi.stubGlobal('ResizeObserver', TestResizeObserver)
  })

  it('keeps a newly saved note when an older refresh resolves later', async () => {
    const pending = deferred<ArticleDetailResponse>()
    apiMocks.getArticle.mockReturnValue(pending.promise)
    const cached = detail(42)
    cached.article.capture_note = '旧想法'
    putArticleDetail(cached)
    render(<MemoryRouter initialEntries={['/articles/42']}>
      <Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes>
    </MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: '编辑随手记' }))
    fireEvent.change(screen.getByRole('textbox', { name: '我的随手记' }), { target: { value: '新想法' } })
    fireEvent.click(screen.getByRole('button', { name: '保存随手记' }))
    await screen.findByText('新想法')
    pending.resolve({ ...cached, article: { ...cached.article, title: 'Fresh server title' } })
    await screen.findByText('Fresh server title')
    expect(screen.getByText('新想法')).toBeTruthy()
    expect(screen.queryByText('旧想法')).toBeNull()
  })

  it('keeps a newly saved note when an earlier content refetch completes', async () => {
    apiMocks.getArticle.mockReturnValue(new Promise(() => {}))
    const pending = deferred<{ content: string }>()
    apiMocks.fetchContent.mockReturnValueOnce(pending.promise)
    const cached = detail(42)
    cached.article.capture_note = '旧想法'
    putArticleDetail(cached)
    render(<MemoryRouter initialEntries={['/articles/42']}>
      <Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes>
    </MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: '重新抓取' }))
    fireEvent.click(screen.getByRole('button', { name: '编辑随手记' }))
    fireEvent.change(screen.getByRole('textbox', { name: '我的随手记' }), { target: { value: '新想法' } })
    fireEvent.click(screen.getByRole('button', { name: '保存随手记' }))
    await screen.findByText('新想法')
    pending.resolve({ content: 'Refetched body' })
    await screen.findByText('Refetched body')
    expect(screen.getByText('新想法')).toBeTruthy()
    expect(screen.queryByText('旧想法')).toBeNull()
  })

  it('starts cached unread articles at the top, including same-page navigation', async () => {
    let scrollY = 1800
    vi.spyOn(window, 'scrollY', 'get').mockImplementation(() => scrollY)
    vi.spyOn(window, 'scrollTo').mockImplementation((x: unknown, y?: number) => {
      scrollY = typeof x === 'object' ? (x as ScrollToOptions).top ?? scrollY : y ?? 0
    })
    apiMocks.getArticle.mockImplementation(() => new Promise(() => {}))
    putArticleDetail(detail(42))
    putArticleDetail(detail(43))
    render(
      <MemoryRouter initialEntries={['/articles/42']}>
        <Link to="/articles/43">Next test article</Link>
        <Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes>
      </MemoryRouter>,
    )
    expect(scrollY).toBe(0)
    scrollY = 1200
    fireEvent.click(screen.getByText('Next test article'))
    await waitFor(() => expect(apiMocks.getArticle).toHaveBeenCalledWith(43))
    expect(scrollY).toBe(0)
    expect(apiMocks.updateProgress).not.toHaveBeenCalled()
  })

  it('clears the list offset before restoring this article saved reading position', async () => {
    let scrollY = 1800
    vi.spyOn(window, 'scrollY', 'get').mockImplementation(() => scrollY)
    vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(800)
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(2800)
    vi.spyOn(window, 'scrollTo').mockImplementation((x: unknown, y?: number) => {
      scrollY = typeof x === 'object' ? (x as ScrollToOptions).top ?? scrollY : y ?? 0
    })
    apiMocks.getArticle.mockImplementation(() => new Promise(() => {}))
    const cached = detail(42)
    cached.progress = { id: 1, article_id: 42, scroll_position: 0.4, is_completed: false, last_read_at: '2026-10-12T00:00:00Z' }
    putArticleDetail(cached)
    render(
      <MemoryRouter initialEntries={['/articles/42']}>
        <Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes>
      </MemoryRouter>,
    )
    expect(scrollY).toBe(0)
    await waitFor(() => expect(scrollY).toBe(800))
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

  it('links the detail source to the feed-filtered article list', () => {
    const fresh = deferred<ArticleDetailResponse>()
    apiMocks.getArticle.mockReturnValue(fresh.promise)
    putArticleDetail(detail(42))

    render(
      <MemoryRouter initialEntries={[{
        pathname: '/articles/42',
        state: { from: '/articles?saved=1' },
      }]}>
        <Routes>
          <Route path="/articles/:id" element={<ArticlePage />} />
        </Routes>
      </MemoryRouter>,
    )

    const source = screen.getByRole('link', { name: '查看 Cached Feed 的文章' })
    expect(source.getAttribute('href')).toBe('/articles?saved=1&feed_id=1')
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
    expect(document.title).toBe('Preview title - RSS Pal')
  })

  it('passes the stored primary video identity to article markdown', () => {
    const fresh = deferred<ArticleDetailResponse>()
    apiMocks.getArticle.mockReturnValue(fresh.promise)
    const cached = detail(42)
    cached.article.media_type = 'video/youtube'
    cached.article.media_url = 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=45'
    cached.article.content = '[[video:youtube:dQw4w9WgXcQ?start=90]]'
    putArticleDetail(cached)

    render(
      <MemoryRouter initialEntries={['/articles/42']}>
        <Routes>
          <Route path="/articles/:id" element={<ArticlePage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(markdownMocks.render).toHaveBeenCalledWith({
      source: '[[video:youtube:dQw4w9WgXcQ?start=90]]',
      imageDimensions: undefined,
      suppressVideo: {
        platform: 'youtube',
        id: 'dQw4w9WgXcQ',
      },
    })
    expect(markdownMocks.findVideoAnchor).toHaveBeenCalledWith(
      '[[video:youtube:dQw4w9WgXcQ?start=90]]',
      { platform: 'youtube', id: 'dQw4w9WgXcQ' },
    )
    expect(playerCardMocks.render.mock.calls.at(-1)?.[0].articleAnchorID).toBe('article-section-001')
  })

  it('invalidates the cached detail after a partial article mutation', async () => {
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

    fireEvent.click(screen.getByRole('button', { name: '重新抓取' }))
    await waitFor(() => expect(apiMocks.fetchContent).toHaveBeenCalledWith(42))
    expect(peekArticleDetail(42)).toBeUndefined()
  })
})

it.each([401, 403, 404])('removes cached private content when the server returns %s', async status => {
  resetArticleDetailCache()
  apiMocks.getArticle.mockRejectedValue({ response: { status } })
  putArticleDetail(detail(42))
  render(<MemoryRouter initialEntries={['/articles/42']}><Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes></MemoryRouter>)
  await waitFor(() => expect(screen.queryByTestId('article-body')).toBeNull())
  expect(screen.getByText('文章不存在或无权访问')).toBeTruthy()
  expect(peekArticleDetail(42)).toBeUndefined()
})
