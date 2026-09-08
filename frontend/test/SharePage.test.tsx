import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import SharePage from '../src/pages/SharePage'

const axiosMock = vi.hoisted(() => ({ get: vi.fn() }))
const privateAPIMocks = vi.hoisted(() => ({
  updateProgress: vi.fn(),
  recordReadDuration: vi.fn(),
  getArticleTags: vi.fn(),
  recordExploreArticleEvent: vi.fn(),
}))

vi.mock('axios', () => ({ default: axiosMock }))
vi.mock('../src/api/client', () => privateAPIMocks)

function sharedSnapshot(overrides: Record<string, unknown> = {}) {
  return {
    title: 'Shared title',
    url: 'https://source.example/post?from=share',
    feed_title: 'Source feed',
    published_at: '2026-09-08T01:02:03Z',
    word_count: 1234,
    reading_minutes: 6,
    summary_brief: 'Brief summary',
    summary_detailed: 'Detailed summary',
    content: '# Full body\n\n[Outbound](https://outside.example/read)',
    snapshotted_at: '2026-09-08T02:03:04Z',
    ...overrides,
  }
}

function NavigateToFresh() {
  const navigate = useNavigate()
  return <button onClick={() => navigate('/share/fresh_token')}>next token</button>
}

function renderSharePage(path = '/share/v1_token', withNavigator = false) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      {withNavigator && <NavigateToFresh />}
      <Routes>
        <Route path="/share/:token" element={<SharePage />} />
      </Routes>
    </MemoryRouter>,
  )
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((onResolve, onReject) => {
    resolve = onResolve
    reject = onReject
  })
  return { promise, resolve, reject }
}

beforeEach(() => {
  axiosMock.get.mockReset()
  Object.values(privateAPIMocks).forEach(mock => mock.mockReset())
})

afterEach(() => {
  document.querySelectorAll('meta[name="referrer"]').forEach(meta => meta.remove())
})

describe('SharePage public reader', () => {
  it('renders source, published and reading metadata, both summaries, full markdown, and safe outbound links', async () => {
    axiosMock.get.mockResolvedValue({ data: sharedSnapshot() })
    renderSharePage()

    expect(await screen.findByRole('heading', { name: 'Shared title' })).toBeTruthy()
    expect(screen.getByText('Source feed')).toBeTruthy()
    expect(screen.getByText(/2026/)).toBeTruthy()
    expect(screen.getByText(/1,234 字/)).toBeTruthy()
    expect(screen.getByText(/6 分钟/)).toBeTruthy()
    expect(screen.getByText('Brief summary')).toBeTruthy()
    expect(screen.getByText('Detailed summary')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Full body' })).toBeTruthy()

    expect(axiosMock.get).toHaveBeenCalledWith('/api/share/v1_token')
    const original = screen.getByRole('link', { name: '阅读原文' })
    expect(original.getAttribute('href')).toBe('https://source.example/post?from=share')
    expect(original.getAttribute('target')).toBe('_blank')
    expect(original.getAttribute('rel')).toBe('noopener noreferrer')
    const markdownLink = screen.getByRole('link', { name: 'Outbound' })
    expect(markdownLink.getAttribute('target')).toBe('_blank')
    expect(markdownLink.getAttribute('rel')).toBe('noopener noreferrer')
  })

  it('encodes the route token and renders both same-origin authentication intents', async () => {
    axiosMock.get.mockResolvedValue({ data: sharedSnapshot() })
    renderSharePage('/share/token%2Fwith%20spaces')

    await screen.findByRole('link', { name: '使用 RSS Pal' })
    expect(screen.getByRole('link', { name: '使用 RSS Pal' }).getAttribute('href')).toBe('/login?intent=use')
    expect(screen.getByRole('link', { name: '订阅原始来源' }).getAttribute('href')).toBe(
      '/login?intent=subscribe&source=https%3A%2F%2Fsource.example%2Fpost%3Ffrom%3Dshare',
    )
    expect(axiosMock.get).toHaveBeenCalledWith('/api/share/token%2Fwith%20spaces')
  })

  it('does not forward an oversized source value into the authentication intent', async () => {
    axiosMock.get.mockResolvedValue({ data: sharedSnapshot({ url: `https://source.example/${'x'.repeat(2049)}` }) })
    renderSharePage()

    const subscribe = await screen.findByRole('link', { name: '订阅原始来源' })
    expect(subscribe.getAttribute('href')).toBe('/login?intent=subscribe')
  })

  it('limits the encoded subscription source and does not double-encode a normal source', async () => {
    axiosMock.get.mockResolvedValue({ data: sharedSnapshot({ url: `https://source.example/${'中'.repeat(2000)}` }) })
    const oversized = renderSharePage()
    expect((await screen.findByRole('link', { name: '订阅原始来源' })).getAttribute('href')).toBe(
      '/login?intent=subscribe',
    )
    oversized.unmount()

    axiosMock.get.mockResolvedValue({ data: sharedSnapshot({ url: 'https://source.example/文章?q=阅读' }) })
    renderSharePage()
    const normal = await screen.findByRole('link', { name: '订阅原始来源' })
    expect(normal.getAttribute('href')).toBe(
      `/login?intent=subscribe&source=${encodeURIComponent('https://source.example/文章?q=阅读')}`,
    )
    expect(normal.getAttribute('href')).not.toContain('%25E6')
  })

  it('opens public summary links safely while preserving internal article-anchor navigation', async () => {
    axiosMock.get.mockResolvedValue({
      data: sharedSnapshot({
        summary_brief: '[Brief external](https://brief.example/read) [Jump](#article-section-001)',
        summary_detailed: '[Detailed external](https://detailed.example/read)',
      }),
    })
    renderSharePage()

    const brief = await screen.findByRole('link', { name: 'Brief external' })
    const detailed = screen.getByRole('link', { name: 'Detailed external' })
    for (const link of [brief, detailed]) {
      expect(link.getAttribute('target')).toBe('_blank')
      expect(link.getAttribute('rel')).toBe('noopener noreferrer')
    }
    const internal = screen.getByRole('link', { name: '跳转原文' })
    expect(internal.getAttribute('href')).toBe('#article-section-001')
    expect(internal.getAttribute('target')).toBeNull()
    expect(internal.getAttribute('rel')).toBeNull()
  })

  it('uses VideoEmbed for a public stored YouTube embed', async () => {
    axiosMock.get.mockResolvedValue({
      data: sharedSnapshot({
        media_url: 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=12',
        media_type: 'video/youtube',
      }),
    })
    renderSharePage()

    const frame = await screen.findByTitle('youtube video dQw4w9WgXcQ')
    expect(frame.getAttribute('src')).toContain('https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ')
    expect(frame.getAttribute('referrerpolicy')).toBe('no-referrer')
  })

  it('uses a native audio element for public http(s) audio', async () => {
    axiosMock.get.mockResolvedValue({
      data: sharedSnapshot({
        media_url: 'https://media.example/episode.mp3',
        media_type: 'audio/mpeg',
        media_duration_seconds: 125,
      }),
    })
    const { container } = renderSharePage()

    await screen.findByRole('heading', { name: 'Shared title' })
    const audio = container.querySelector('audio')
    expect(audio?.getAttribute('src')).toBe('https://media.example/episode.mp3')
    expect(audio?.hasAttribute('controls')).toBe(true)
    expect(screen.getByText('2分05秒')).toBeTruthy()
  })

  it.each([
    ['HTTP://media.example/episode.mp3', 'http://media.example/episode.mp3'],
    ['  https://media.example/episode.mp3  ', 'https://media.example/episode.mp3'],
  ])('accepts and normalizes public audio URL %s', async (mediaURL, expected) => {
    axiosMock.get.mockResolvedValue({
      data: sharedSnapshot({ media_url: mediaURL, media_type: 'audio/mpeg' }),
    })
    const { container } = renderSharePage()

    await screen.findByRole('heading', { name: 'Shared title' })
    expect(container.querySelector('audio')?.getAttribute('src')).toBe(expected)
  })

  it.each([
    ['/api/media/youtube/private-ticket', 'video/youtube'],
    ['/local/media.mp3', 'audio/mpeg'],
    ['file:///etc/passwd', 'audio/mpeg'],
    ['javascript:alert(1)', 'audio/mpeg'],
    ['data:audio/mpeg;base64,AAAA', 'audio/mpeg'],
    ['blob:https://source.example/id', 'audio/mpeg'],
    ['//media.example/episode.mp3', 'audio/mpeg'],
    ['https://user:secret@media.example/episode.mp3', 'audio/mpeg'],
    ['https://rss.example/api/media/youtube/private-ticket', 'audio/mpeg'],
  ])('replaces non-public media %s with an origin link and no playback surface', async (mediaURL, mediaType) => {
    axiosMock.get.mockResolvedValue({
      data: sharedSnapshot({ media_url: mediaURL, media_type: mediaType }),
    })
    const { container } = renderSharePage()

    const fallback = await screen.findByRole('link', { name: '前往原网站播放' })
    expect(fallback.getAttribute('href')).toBe('https://source.example/post?from=share')
    expect(fallback.getAttribute('target')).toBe('_blank')
    expect(fallback.getAttribute('rel')).toBe('noopener noreferrer')
    expect(container.querySelector('video,audio,iframe')).toBeNull()
  })

  it('never calls progress, preference, event, or tag APIs', async () => {
    axiosMock.get.mockResolvedValue({ data: sharedSnapshot() })
    renderSharePage()
    await screen.findByRole('heading', { name: 'Full body' })

    expect(privateAPIMocks.updateProgress).not.toHaveBeenCalled()
    expect(privateAPIMocks.recordReadDuration).not.toHaveBeenCalled()
    expect(privateAPIMocks.getArticleTags).not.toHaveBeenCalled()
    expect(privateAPIMocks.recordExploreArticleEvent).not.toHaveBeenCalled()
  })

  it('shows the exact unavailable message without retry for a 404', async () => {
    axiosMock.get.mockRejectedValue({ response: { status: 404 } })
    renderSharePage('/share/missing')

    expect(await screen.findByText('分享链接无效或已过期')).toBeTruthy()
    expect(screen.queryByRole('button', { name: '重试' })).toBeNull()
  })

  it.each([
    new Error('network'),
    { response: { status: 500 } },
  ])('offers retry after a retryable failure and can recover', async error => {
    axiosMock.get
      .mockRejectedValueOnce(error)
      .mockResolvedValueOnce({ data: sharedSnapshot() })
    renderSharePage()

    fireEvent.click(await screen.findByRole('button', { name: '重试' }))
    expect(await screen.findByRole('heading', { name: 'Shared title' })).toBeTruthy()
    expect(axiosMock.get).toHaveBeenCalledTimes(2)
  })

  it('does not let a stale token response replace the newer snapshot', async () => {
    const stale = deferred<{ data: ReturnType<typeof sharedSnapshot> }>()
    const fresh = deferred<{ data: ReturnType<typeof sharedSnapshot> }>()
    axiosMock.get
      .mockReturnValueOnce(stale.promise)
      .mockReturnValueOnce(fresh.promise)
    renderSharePage('/share/stale_token', true)

    fireEvent.click(screen.getByRole('button', { name: 'next token' }))
    fresh.resolve({ data: sharedSnapshot({ title: 'Fresh title' }) })
    expect(await screen.findByRole('heading', { name: 'Fresh title' })).toBeTruthy()

    stale.resolve({ data: sharedSnapshot({ title: 'Stale title' }) })
    await act(async () => { await stale.promise })
    expect(screen.queryByRole('heading', { name: 'Stale title' })).toBeNull()
    expect(screen.getByRole('heading', { name: 'Fresh title' })).toBeTruthy()
  })

  it('blocks unsafe markdown link protocols and never emits unsafe media src values', async () => {
    axiosMock.get.mockResolvedValue({
      data: sharedSnapshot({
        content: '[bad](javascript:alert(1))',
        media_url: 'file:///etc/passwd',
        media_type: 'audio/mpeg',
      }),
    })
    const { container } = renderSharePage()

    await screen.findByRole('heading', { name: 'Shared title' })
    expect(container.querySelector('a[href^="javascript:"]')).toBeNull()
    expect(container.querySelector('audio[src^="file:"],video[src^="file:"],iframe[src^="file:"]')).toBeNull()
  })

  it('installs no-referrer metadata and restores an existing value plus the document title', async () => {
    const meta = document.createElement('meta')
    meta.name = 'referrer'
    meta.content = 'origin'
    document.head.append(meta)
    document.title = 'Before share'
    axiosMock.get.mockResolvedValue({ data: sharedSnapshot() })

    const view = renderSharePage()
    await waitFor(() => expect(document.title).toBe('Shared title - RSS Pal'))
    expect(meta.content).toBe('no-referrer')

    view.unmount()
    expect(document.title).toBe('Before share')
    expect(meta.isConnected).toBe(true)
    expect(meta.content).toBe('origin')
  })

  it('removes a created referrer element and ignores a late response after unmount', async () => {
    const response = deferred<{ data: ReturnType<typeof sharedSnapshot> }>()
    axiosMock.get.mockReturnValue(response.promise)
    document.title = 'Before late response'

    const view = renderSharePage()
    expect(document.querySelector('meta[name="referrer"]')?.getAttribute('content')).toBe('no-referrer')
    view.unmount()
    expect(document.querySelector('meta[name="referrer"]')).toBeNull()
    expect(document.title).toBe('Before late response')

    response.resolve({ data: sharedSnapshot({ title: 'Too late' }) })
    await act(async () => { await response.promise })
    expect(document.title).toBe('Before late response')
  })
})
