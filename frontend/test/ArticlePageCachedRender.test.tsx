import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ArticlePage from '../src/pages/ArticlePage'
import {
  peekArticleDetail,
  putArticleDetail,
  resetArticleDetailCache,
} from '../src/api/articleDetailCache'
import { saveDraftLinks } from '../src/utils/linkSetSelection'
import type { Article, ArticleDetailResponse, ArticleListItem } from '../src/api/client'

const apiMocks = vi.hoisted(() => ({
  getArticleCaptureDrafts: vi.fn(async (): Promise<{drafts: Array<{url:string;title:string;addedAt:number}>; fetched_link_urls: string[]}> => ({drafts:[], fetched_link_urls:[]})),
  removeArticleCaptureDrafts: vi.fn(async () => undefined),
  batchFetchCandidates: vi.fn(async () => ({inserted:2})),
  updateArticleNote: vi.fn(async () => ({ capture_note: '新想法' })),
  dislikeArticle: vi.fn(async () => undefined),
  expandLinkSetChild: vi.fn(async () => undefined),
  exportMarkdown: vi.fn(async () => ''),
  fetchContent: vi.fn(async (): Promise<{ content: string; content_changed?: boolean; published_at?: string | null; message?: string }> => ({ content: '' })),
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
vi.mock('../src/components/LinkSetChildren', () => ({
  LinkSetChildren: () => null,
}))
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
  it('polls a queued capture until the fetched body arrives', async () => {
    vi.useFakeTimers()
    try {
      const queued = detail(42)
      queued.article.content = ''
      queued.article.processing_state = 'queued'
      putArticleDetail(queued)
      apiMocks.getArticle.mockResolvedValue(queued)
      render(<MemoryRouter initialEntries={['/articles/42']}><Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes></MemoryRouter>)
      await act(async () => {})
      expect(screen.getByText('已加入抓取队列，后台完成后正文会自动显示。')).toBeTruthy()
      expect(screen.queryByText(/这篇 PDF 正在 OCR/)).toBeNull()
      await act(async () => { await vi.advanceTimersByTimeAsync(5 * 60 * 1000 + 4000) })
      const done = detail(42)
      done.article.processing_state = 'ready'
      done.article.content = 'Fetched queued article body'
      apiMocks.getArticle.mockResolvedValue(done)
      await act(async () => { await vi.advanceTimersByTimeAsync(4000) })
      expect(screen.getByTestId('article-body').textContent).toBe('Fetched queued article body')
      expect(apiMocks.expandLinkSetChild).not.toHaveBeenCalled()
    } finally { vi.useRealTimers() }
  })
  beforeEach(() => {
    resetArticleDetailCache()
    localStorage.clear()
    vi.clearAllMocks()
    apiMocks.getArticleCaptureDrafts.mockResolvedValue({drafts:[], fetched_link_urls:[]})
    apiMocks.fetchContent.mockReset().mockResolvedValue({ content: '' })
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    vi.stubGlobal('ResizeObserver', TestResizeObserver)
  })

  it('imports shortcut rows into the existing pending badge and waits for explicit Start Fetch', async () => {
    const imported = {url:'https://example.com/shortcut',title:'Shortcut link',addedAt:123}
    const manual = {url:'https://example.com/manual',title:'Manual link',addedAt:100}
    saveDraftLinks(42, [manual])
    apiMocks.getArticle.mockReturnValue(new Promise(() => {}))
    putArticleDetail(detail(42))
    render(<MemoryRouter initialEntries={['/articles/42']}><Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes></MemoryRouter>)
    await screen.findByTitle('1 条链接待确认抓取')
    apiMocks.getArticleCaptureDrafts.mockResolvedValue({drafts:[imported],fetched_link_urls:[]})
    await act(async () => { window.dispatchEvent(new Event('focus')) })
    fireEvent.click(await screen.findByTitle('2 条链接待确认抓取'))
    expect(screen.getByRole('dialog').textContent).toContain('Shortcut link')
    expect(screen.getByRole('dialog').textContent).toContain('Manual link')
    expect(apiMocks.batchFetchCandidates).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', {name:'开始抓取（2）'}))
    await waitFor(() => expect(apiMocks.batchFetchCandidates).toHaveBeenCalledWith(42, [
      {url:manual.url,title:manual.title},{url:imported.url,title:imported.title},
    ]))
    await waitFor(() => expect(screen.queryByTitle('2 条链接待确认抓取')).toBeNull())
    await act(async () => { window.dispatchEvent(new Event('focus')) })
    expect(screen.queryByTitle('2 条链接待确认抓取')).toBeNull()
    expect(apiMocks.batchFetchCandidates).toHaveBeenCalledTimes(1)
  })

  it('ignores a submitted batch completion after navigation to another article', async () => {
    const pending = deferred<{inserted:number}>()
    apiMocks.batchFetchCandidates.mockReturnValueOnce(pending.promise)
    apiMocks.getArticle.mockReturnValue(new Promise(() => {}))
    saveDraftLinks(42, [{url:'https://example.com/shared',title:'Share',addedAt:123}])
    putArticleDetail(detail(42))
    putArticleDetail({...detail(43),article:{...detail(43).article,title:'Next article'}})
    render(<MemoryRouter initialEntries={['/articles/42']}>
      <Link to="/articles/43">Next test article</Link>
      <Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes>
    </MemoryRouter>)
    fireEvent.click(await screen.findByTitle('1 条链接待确认抓取'))
    fireEvent.click(screen.getByRole('button', {name:'开始抓取（1）'}))
    fireEvent.click(screen.getByText('Next test article'))
    await screen.findByText('Next article')
    const requestsBefore = apiMocks.getArticle.mock.calls.length
    await act(async () => { pending.resolve({inserted:1}) })
    expect(apiMocks.getArticle.mock.calls.length).toBe(requestsBefore)
    expect(screen.getByText('Next article')).toBeTruthy()
  })

  it('keeps an imported row visible and reports a failed remote removal', async () => {
    apiMocks.getArticle.mockReturnValue(new Promise(() => {}))
    apiMocks.getArticleCaptureDrafts.mockResolvedValue({drafts:[{url:'https://example.com/shared',title:'Shortcut link',addedAt:123}],fetched_link_urls:[]})
    apiMocks.removeArticleCaptureDrafts.mockRejectedValueOnce(new Error('offline'))
    putArticleDetail(detail(42))
    const messages: string[] = []
    const listener = (event: Event) => { messages.push((event as CustomEvent).detail.msg) }
    window.addEventListener('show-toast', listener)
    try {
      render(<MemoryRouter initialEntries={['/articles/42']}><Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes></MemoryRouter>)
      fireEvent.click(await screen.findByTitle('1 条链接待确认抓取'))
      fireEvent.click(screen.getByRole('button', {name:'从草稿移除 Shortcut link'}))
      await waitFor(() => expect(messages).toContain('移除失败，请联网后重试'))
      expect(screen.getByRole('dialog').textContent).toContain('Shortcut link')
      expect(apiMocks.removeArticleCaptureDrafts).toHaveBeenCalledWith(42, ['https://example.com/shared'])
      expect(apiMocks.batchFetchCandidates).not.toHaveBeenCalled()
    } finally { window.removeEventListener('show-toast', listener) }
  })

  it('offers accessible thumb icons and keeps feedback selection mutually exclusive', async () => {
    apiMocks.getArticle.mockReturnValue(new Promise(() => {}))
    putArticleDetail(detail(42))
    render(<MemoryRouter initialEntries={['/articles/42']}>
      <Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes>
    </MemoryRouter>)
    const like = screen.getByRole('button', { name: '喜欢', exact: true })
    const dislike = screen.getByRole('button', { name: '不喜欢', exact: true })
    expect(like.querySelector('svg')).toBeTruthy()
    expect(dislike.querySelector('svg')).toBeTruthy()
    expect(like.textContent).toBe('')
    expect(dislike.textContent).toBe('')
    expect(like.getAttribute('aria-pressed')).toBe('false')
    expect(dislike.getAttribute('aria-pressed')).toBe('false')
    fireEvent.click(like)
    await waitFor(() => expect(like.getAttribute('aria-pressed')).toBe('true'))
    expect(apiMocks.likeArticle).toHaveBeenCalledWith(42)
    fireEvent.click(dislike)
    await waitFor(() => expect(dislike.getAttribute('aria-pressed')).toBe('true'))
    expect(like.getAttribute('aria-pressed')).toBe('false')
    expect(apiMocks.dislikeArticle).toHaveBeenCalledWith(42)
    fireEvent.click(dislike)
    await waitFor(() => expect(dislike.getAttribute('aria-pressed')).toBe('false'))
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

  it.each([true, false])('updates visible summaries when content_changed=%s', async changed => {
    apiMocks.getArticle.mockReturnValue(new Promise(() => {}))
    apiMocks.fetchContent.mockResolvedValueOnce({ content: changed ? 'Refetched body' : 'Cached body', content_changed: changed })
    putArticleDetail(detail(42))
    render(<MemoryRouter initialEntries={['/articles/42']}><Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes></MemoryRouter>)
    expect(screen.getByText('Cached brief')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '重新抓取' }))
    await waitFor(() => expect(playerCardMocks.render.mock.calls.at(-1)?.[0].article.summary_brief).toBe(changed ? '' : 'Cached brief'))
    if (changed) expect(screen.queryByText('Cached brief')).toBeNull()
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

  function renderClip() {
    apiMocks.getArticle.mockReturnValue(new Promise(() => {}))
    putArticleDetail({ ...detail(42), from_bookmarklet: true })
    putArticleDetail({ ...detail(43), from_bookmarklet: true, article: { ...detail(43).article, content: 'Next body' } })
    render(<MemoryRouter initialEntries={['/articles/42']}>
      <Link to="/articles/43">Next test article</Link>
      <Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes>
    </MemoryRouter>)
  }

  it('refetches a web clip on the backend without opening the source page', async () => {
    const pending = deferred<{ content: string; published_at: string }>()
    apiMocks.fetchContent.mockReturnValueOnce(pending.promise)
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const confirm = vi.spyOn(window, 'confirm')
    renderClip()
    const button = screen.getByRole('button', { name: /重新抓取/ })
    fireEvent.click(button)
    expect(apiMocks.fetchContent).toHaveBeenCalledWith(42)
    expect((button as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(button)
    expect(apiMocks.fetchContent).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('link', { name: '打开原网页手动抓取' })).toBeNull()
    pending.resolve({ content: 'Refetched clip', published_at: '2026-09-20T08:30:00+08:00' })
    await screen.findByText('Refetched clip')
    expect(playerCardMocks.render.mock.calls.at(-1)?.[0].article.published_at).toBe('2026-09-20T08:30:00+08:00')
    expect(open).not.toHaveBeenCalled()
    expect(confirm).not.toHaveBeenCalled()
    expect((button as HTMLButtonElement).disabled).toBe(false)
  })

  it.each(['request error', 'no content found', 'empty content'])('offers manual capture only after %s', async reason => {
    if (reason === 'request error') apiMocks.fetchContent.mockRejectedValueOnce(new Error('timeout'))
    else apiMocks.fetchContent.mockResolvedValueOnce(reason === 'no content found'
      ? { content: 'Cached body', message: 'no content found' }
      : { content: '  ' })
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    renderClip()
    fireEvent.click(screen.getByRole('button', { name: /重新抓取/ }))
    const manual = await screen.findByRole('link', { name: '打开原网页手动抓取' })
    expect(screen.getByRole('alert').textContent).toContain('扩展或书签')
    expect(manual.getAttribute('href')).toBe('https://example.com/42')
    expect(manual.getAttribute('target')).toBe('_blank')
    expect(manual.getAttribute('rel')).toBe('noopener noreferrer')
    expect(screen.getByTestId('article-body').textContent).toBe('Cached body')
    expect(open).not.toHaveBeenCalled()
    apiMocks.fetchContent.mockResolvedValueOnce({ content: 'Retry succeeded' })
    fireEvent.click(screen.getByRole('button', { name: '重新抓取' }))
    await screen.findByText('Retry succeeded')
    expect(screen.queryByRole('link', { name: '打开原网页手动抓取' })).toBeNull()
  })

  it('clears the manual fallback when navigating to another article', async () => {
    apiMocks.fetchContent.mockRejectedValueOnce(new Error('timeout'))
    renderClip()
    fireEvent.click(screen.getByRole('button', { name: /重新抓取/ }))
    await screen.findByRole('link', { name: '打开原网页手动抓取' })
    fireEvent.click(screen.getByText('Next test article'))
    await screen.findByText('Next body')
    expect(screen.queryByRole('link', { name: '打开原网页手动抓取' })).toBeNull()
  })

  it.each(['success', 'failure'])('ignores a late refetch %s after navigating away', async outcome => {
    const pending = deferred<{ content: string }>()
    apiMocks.fetchContent.mockReturnValueOnce(pending.promise.then(result => {
      if (outcome === 'failure') throw new Error('timeout')
      return result
    }))
    renderClip()
    fireEvent.click(screen.getByRole('button', { name: /重新抓取/ }))
    expect(apiMocks.fetchContent).toHaveBeenCalledWith(42)
    fireEvent.click(screen.getByText('Next test article'))
    await screen.findByText('Next body')
    expect((screen.getByRole('button', { name: '重新抓取' }) as HTMLButtonElement).disabled).toBe(false)
    await act(async () => pending.resolve({ content: 'Previous article result' }))
    expect(screen.getByTestId('article-body').textContent).toBe('Next body')
    expect(screen.queryByRole('link', { name: '打开原网页手动抓取' })).toBeNull()
  })

  it('copies a public share directly beside Share without opening the dialog', async () => {
    apiMocks.getArticle.mockImplementation(() => new Promise(() => {}))
    putArticleDetail(detail(42))
    const url = 'https://r.morefreeze.top/Aa0000000000'
    apiMocks.createArticleShare.mockResolvedValueOnce({id:'a',url,status:'active',legacy:false,expires_at:null})
    Object.defineProperty(navigator, 'clipboard', {configurable:true,value:{writeText:vi.fn().mockResolvedValue(undefined)}})
    render(<MemoryRouter initialEntries={['/articles/42']}><Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes></MemoryRouter>)
    const share = screen.getByRole('button', {name:'分享'})
    const copy = screen.getByRole('button', {name:'复制链接'})
    expect(share.nextElementSibling?.contains(copy)).toBe(true)
    fireEvent.click(copy)
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(url))
    expect(apiMocks.createArticleShare).toHaveBeenCalledWith(42, null)
    expect(screen.queryByRole('dialog')).toBeNull()
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
      originalUrl: 'https://example.com/42',
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

  it.each([
    { name: 'newly extracted date', response: { content: 'Fresh body', published_at: '2026-09-20T08:30:00+08:00' }, expected: '2026-09-20T08:30:00+08:00' },
    { name: 'older endpoint without date', response: { content: 'Fresh body' }, expected: '2026-07-27T00:00:00Z' },
    { name: 'explicitly absent date', response: { content: 'Fresh body', published_at: null }, expected: null },
  ])('updates refetched content and publication display for $name', async ({ response, expected }) => {
    apiMocks.getArticle.mockReturnValue(new Promise(() => {}))
    apiMocks.fetchContent.mockResolvedValueOnce(response)
    putArticleDetail(detail(42))
    render(
      <MemoryRouter initialEntries={['/articles/42']}>
        <Routes><Route path="/articles/:id" element={<ArticlePage />} /></Routes>
      </MemoryRouter>,
    )

    const publicationDisplay = screen.getByRole('link', { name: '原文链接' }).parentElement!.querySelector('span')!
    const oldDisplay = publicationDisplay.textContent
    fireEvent.click(screen.getByRole('button', { name: '重新抓取' }))
    await waitFor(() => expect(screen.getByTestId('article-body').textContent).toBe('Fresh body'))
    expect(playerCardMocks.render.mock.calls.at(-1)?.[0].article.published_at).toBe(expected)
    if (expected === response.published_at) {
      expect(publicationDisplay.textContent).not.toBe(oldDisplay)
    } else {
      expect(publicationDisplay.textContent).toBe(oldDisplay)
    }
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
