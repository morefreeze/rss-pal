import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Link, MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import ArticleListPage from '../src/pages/ArticleListPage'
import { ReadingWorkspaceContext, type ReadingWorkspaceValue } from '../src/workspace/ReadingWorkspaceContext'

const api = vi.hoisted(() => ({ getArticles: vi.fn(), getFeeds: vi.fn(), getTagSidebar: vi.fn() }))
vi.mock('../src/api/client', async original => ({ ...await original<typeof import('../src/api/client')>(), ...api }))
vi.mock('../src/player/PlayerContext', () => ({ usePlayer: () => ({ playArticle: vi.fn() }) }))
vi.mock('../src/pages/ClipPage', () => ({ default: ({ restrictToFeedId }: { restrictToFeedId?: number }) => <output data-testid="clip-feed">{restrictToFeedId ?? 'pending'}</output> }))
vi.mock('../src/hooks/useInfiniteScrollTrigger', () => ({ useInfiniteScrollTrigger: () => {} }))
vi.mock('../src/hooks/useArticleDetailPrefetch', () => ({ useArticleDetailPrefetch: () => ({ promote: () => {}, registerCard: () => {} }) }))

const closeReader = vi.fn()
function context(overrides: Partial<ReadingWorkspaceValue> = {}): ReadingWorkspaceValue {
  return {
    activeModule: 'articles', isActive: true, isListVisible: true, isReaderOpen: false,
    selectedArticleId: null, isPhone: false, isCompact: true, listPath: '/articles', returnPath: '/articles',
    getListScrollElement: () => null, closeReader, openScopes: () => {}, sidebarTarget: null, overviewTarget: null,
    ...overrides,
  }
}
function LocationProbe() { const location = useLocation(); return <output data-testid="location">{location.pathname}{location.search}</output> }
beforeEach(() => {
  vi.clearAllMocks()
  sessionStorage.clear()
  localStorage.clear()
  api.getArticles.mockResolvedValue([])
  api.getFeeds.mockResolvedValue([])
  api.getTagSidebar.mockResolvedValue({ total_count: 0, untagged_count: 0, tags: [] })
  window.scrollTo = vi.fn()
})

it('does not close an open clip reader when its feed resolves asynchronously', async () => {
  let resolveFeeds!: (feeds: unknown[]) => void
  api.getFeeds.mockReturnValue(new Promise(resolve => { resolveFeeds = resolve }))
  render(<MemoryRouter initialEntries={['/articles?view=clip']}><ReadingWorkspaceContext.Provider value={context({ activeModule: 'clips', isReaderOpen: true, selectedArticleId: 42, listPath: '/articles?view=clip' })}><ArticleListPage /></ReadingWorkspaceContext.Provider></MemoryRouter>)
  expect(screen.queryByTestId('clip-feed')).toBeNull()
  await act(async () => { resolveFeeds([{ id: 12, title: 'Clippings', url: 'clip://12', feed_type: 'clip', is_active: true }]) })
  await waitFor(() => expect(screen.getByTestId('clip-feed').textContent).toBe('12'))
  expect(closeReader).not.toHaveBeenCalled()
})

it('refreshes retained feed scopes and articles after subscriptions change', async () => {
  render(<MemoryRouter initialEntries={['/articles']}><ReadingWorkspaceContext.Provider value={context()}><ArticleListPage /></ReadingWorkspaceContext.Provider></MemoryRouter>)
  await waitFor(() => expect(api.getFeeds).toHaveBeenCalledTimes(1))
  await waitFor(() => expect(api.getArticles).toHaveBeenCalled())
  api.getFeeds.mockClear()
  api.getArticles.mockClear()
  await act(async () => { window.dispatchEvent(new Event('subscriptions-changed')) })
  await waitFor(() => expect(api.getFeeds).toHaveBeenCalledTimes(1))
  await waitFor(() => expect(api.getArticles).toHaveBeenCalledWith(expect.objectContaining({ offset: 0 })))
  expect(closeReader).not.toHaveBeenCalled()
})

it('applies the legacy saved URL to a retained list and consumes the redirect parameter', async () => {
  render(<MemoryRouter initialEntries={['/articles']}><ReadingWorkspaceContext.Provider value={context()}><Link to="/articles?saved=1">old saved bookmark</Link><LocationProbe /><ArticleListPage /></ReadingWorkspaceContext.Provider></MemoryRouter>)
  await waitFor(() => expect(api.getArticles).toHaveBeenCalled())
  api.getArticles.mockClear()
  fireEvent.click(screen.getByRole('link', { name: 'old saved bookmark' }))
  await waitFor(() => expect(api.getArticles).toHaveBeenCalledWith(expect.objectContaining({ saved: true, unread: undefined })))
  expect(screen.getByTestId('location').textContent).toBe('/articles')
  expect(localStorage.getItem('savedOnly')).toBe('true')
  expect(closeReader).not.toHaveBeenCalled()
})
