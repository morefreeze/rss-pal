import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => {
  const player = {
    initialize: vi.fn(),
    on: vi.fn(),
    off: vi.fn(),
    destroy: vi.fn(),
  }
  return {
    player,
    create: vi.fn(() => player),
    start: vi.fn(),
  }
})

vi.mock('dashjs', () => ({
  MediaPlayer: Object.assign(
    () => ({ create: mocks.create }),
    { events: { ERROR: 'error' } },
  ),
}))

vi.mock('../src/api/client', () => ({
  startYouTubePlayback: mocks.start,
}))

import YouTubeRelayPlayer from '../src/components/YouTubeRelayPlayer'

function setMediaSourceSupported(supported: boolean) {
  Object.defineProperty(window, 'MediaSource', {
    configurable: true,
    value: supported ? class MediaSource {} : undefined,
  })
}

describe('YouTubeRelayPlayer', () => {
  beforeEach(() => {
    mocks.player.initialize.mockReset()
    mocks.player.on.mockReset()
    mocks.player.off.mockReset()
    mocks.player.destroy.mockReset()
    mocks.create.mockClear()
    mocks.start.mockReset()
    setMediaSourceSupported(true)
  })

  afterEach(() => {
    Object.defineProperty(window, 'MediaSource', {
      configurable: true,
      value: undefined,
    })
  })

  it('starts an authenticated DASH session and initializes a seekable video player', async () => {
    mocks.start.mockResolvedValue({
      manifest_url: '/api/media/youtube/ticket/manifest.mpd',
      progressive_url: '/api/media/youtube/ticket/progressive',
      mode: 'dash',
      quality: 1080,
      progressive_quality: 720,
      expires_at: '2026-07-30T18:00:00Z',
    })

    render(<YouTubeRelayPlayer articleId={2391} originalURL="https://youtube.com/watch?v=abc" />)

    expect(await screen.findByText('1080p · 北京中转')).toBeTruthy()
    const video = screen.getByLabelText('YouTube 视频播放器')
    expect(video.getAttribute('controls')).not.toBeNull()
    expect(mocks.start).toHaveBeenCalledWith(2391, expect.any(AbortSignal))
    expect(mocks.player.on).toHaveBeenCalledWith('error', expect.any(Function))
    expect(mocks.player.initialize).toHaveBeenCalledWith(
      video,
      '/api/media/youtube/ticket/manifest.mpd',
      false,
    )
  })

  it('uses the progressive fallback when MediaSource is unavailable', async () => {
    setMediaSourceSupported(false)
    mocks.start.mockResolvedValue({
      progressive_url: '/api/media/youtube/ticket/progressive',
      mode: 'dash',
      quality: 720,
      progressive_quality: 720,
      expires_at: '2026-07-30T18:00:00Z',
    })

    render(<YouTubeRelayPlayer articleId={2391} originalURL="https://youtube.com/watch?v=abc" />)

    const video = await screen.findByLabelText('YouTube 视频播放器')
    await waitFor(() => expect(video.getAttribute('src')).toBe('/api/media/youtube/ticket/progressive'))
    expect(mocks.create).not.toHaveBeenCalled()
    expect(screen.getByText('720p · 兼容模式')).toBeTruthy()
  })

  it('falls back to progressive playback after a DASH error', async () => {
    mocks.start.mockResolvedValue({
      manifest_url: '/api/media/youtube/ticket/manifest.mpd',
      progressive_url: '/api/media/youtube/ticket/progressive',
      mode: 'dash',
      quality: 1080,
      progressive_quality: 720,
      expires_at: '2026-07-30T18:00:00Z',
    })

    render(<YouTubeRelayPlayer articleId={2391} originalURL="https://youtube.com/watch?v=abc" />)
    const video = await screen.findByLabelText('YouTube 视频播放器')
    await waitFor(() => expect(mocks.player.on).toHaveBeenCalled())
    const onError = mocks.player.on.mock.calls.find(([event]) => event === 'error')?.[1]
    fireEvent(video, new Event('loadedmetadata'))
    onError?.({ type: 'error' })

    await waitFor(() => expect(video.getAttribute('src')).toBe('/api/media/youtube/ticket/progressive'))
    expect(mocks.player.destroy).toHaveBeenCalled()
    expect(screen.getByText('720p · 兼容模式')).toBeTruthy()
  })

  it('shows a retry action when session creation fails', async () => {
    mocks.start
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValueOnce({
        progressive_url: '/api/media/youtube/ticket/progressive',
        mode: 'progressive',
        quality: 720,
        expires_at: '2026-07-30T18:00:00Z',
      })

    render(<YouTubeRelayPlayer articleId={2391} originalURL="https://youtube.com/watch?v=abc" />)

    fireEvent.click(await screen.findByRole('button', { name: '重试播放' }))
    expect((await screen.findByLabelText('YouTube 视频播放器')).getAttribute('src')).toBe(
      '/api/media/youtube/ticket/progressive',
    )
    expect(mocks.start).toHaveBeenCalledTimes(2)
  })

  it('aborts session creation when the player unmounts', async () => {
    mocks.start.mockReturnValue(new Promise(() => {}))

    const { unmount } = render(
      <YouTubeRelayPlayer articleId={2391} originalURL="https://youtube.com/watch?v=abc" />,
    )
    await waitFor(() => expect(mocks.start).toHaveBeenCalled())
    const signal = mocks.start.mock.calls[0][1] as AbortSignal

    unmount()

    expect(signal.aborted).toBe(true)
  })
})
