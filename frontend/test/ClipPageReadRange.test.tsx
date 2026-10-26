import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ClipPage from '../src/pages/ClipPage'
const mocks = vi.hoisted(() => ({
  getClip: vi.fn(),
  listTags: vi.fn(),
  markReadRange: vi.fn(),
  invalidateArticleDetail: vi.fn(),
}))
vi.mock('../src/api/client', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  getClip: mocks.getClip,
  listTags: mocks.listTags,
  markReadRange: mocks.markReadRange,
}))
vi.mock('../src/api/articleDetailCache', () => ({
  invalidateArticleDetail: mocks.invalidateArticleDetail,
}))
vi.mock('../src/hooks/useExposureTracking', () => ({
  useExposureTracking: () => ({ current: null }),
  reportClick: vi.fn(),
}))
vi.mock('../src/player/PlayerContext', () => ({
  usePlayer: () => ({ playArticle: vi.fn() }),
}))
const clip = {
  id: 10,
  feed_id: 4,
  title: '测试网摘',
  url: 'https://blog.example/10',
  published_at: null,
  fetched_at: '2026-10-26T00:00:00Z',
  manual_tags: [],
  effective_source: {
    kind: 'host',
    key: 'host:blog.example',
    title: 'blog.example',
  },
}
beforeEach(() => {
  vi.clearAllMocks()
  sessionStorage.clear()
  mocks.listTags.mockResolvedValue([])
  mocks.getClip.mockResolvedValue({ items: [clip], total: 100 })
  mocks.markReadRange.mockResolvedValue({ count: 80, article_ids: [10, 90] })
})
describe('clip range reading', () => {
  it('forwards the embedded feed and sort/checkbox scope and reloads after server range mutation', async () => {
    render(
      <MemoryRouter>
        <ClipPage
          restrictToFeedId={4}
          sortField="captured"
          sortDir="asc"
          unreadOnly
          savedOnly
        />
      </MemoryRouter>,
    )
    fireEvent.click(await screen.findByRole('button', { name: '文章已读操作' }))
    fireEvent.click(
      screen.getByRole('menuitem', { name: '这篇及以下标记已读' }),
    )
    await waitFor(() =>
      expect(mocks.markReadRange).toHaveBeenCalledWith({
        anchor_id: 10,
        direction: 'below',
        sort: 'captured',
        order: 'asc',
        feed_id: 4,
        clip: true,
        source: 'feed:4',
        tag_id: undefined,
        untagged: false,
        unread: true,
        saved: true,
      }),
    )
    await waitFor(() =>
      expect(
        mocks.getClip.mock.calls.filter(
          ([q]) => q.limit === 20 && q.offset === 0,
        ).length,
      ).toBe(2),
    )
    expect(mocks.invalidateArticleDetail).toHaveBeenCalledWith(90)
  })
  it('leaves the list in place and shows a failure message when marking fails', async () => {
    mocks.markReadRange.mockRejectedValue(new Error('offline'))
    const messages: string[] = []
    const listener = (e: Event) => messages.push((e as CustomEvent).detail.msg)
    window.addEventListener('show-toast', listener)
    try {
      render(
        <MemoryRouter>
          <ClipPage restrictToFeedId={4} />
        </MemoryRouter>,
      )
      fireEvent.click(
        await screen.findByRole('button', { name: '文章已读操作' }),
      )
      fireEvent.click(screen.getByRole('menuitem', { name: '这篇标记已读' }))
      await waitFor(() => expect(messages).toContain('标记已读失败，请重试'))
      expect(await screen.findByText('测试网摘')).toBeTruthy()
      expect(sessionStorage.getItem('readArticles')).toBeNull()
    } finally {
      window.removeEventListener('show-toast', listener)
    }
  })
})
