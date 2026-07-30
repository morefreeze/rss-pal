import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { BrowserPlayback } from '../src/youtube/bridge'

const mocks = vi.hoisted(() => {
  const players: Array<{
    initialize: ReturnType<typeof vi.fn>
    on: ReturnType<typeof vi.fn>
    off: ReturnType<typeof vi.fn>
    destroy: ReturnType<typeof vi.fn>
  }> = []

  const createPlayer = () => {
    const player = {
      initialize: vi.fn(),
      on: vi.fn(),
      off: vi.fn(),
      destroy: vi.fn(),
    }
    players.push(player)
    return player
  }

  return {
    detect: vi.fn(),
    resolve: vi.fn(),
    buildMpd: vi.fn(() => '<MPD/>'),
    players,
    create: vi.fn(createPlayer),
    mediaPlayer: vi.fn(() => ({ create: mocks.create })),
    createObjectURL: vi.fn(() => `blob:rss-pal-${mocks.createObjectURL.mock.calls.length}`),
    revokeObjectURL: vi.fn(),
    load: vi.fn(),
    play: vi.fn(),
  }
})

vi.mock('../src/youtube/bridge', () => {
  class YouTubeBridgeError extends Error {
    constructor(public readonly code: string) {
      super(code)
      this.name = 'YouTubeBridgeError'
    }
  }

  return {
    detectYouTubeBridge: mocks.detect,
    resolveYouTubePlayback: mocks.resolve,
    YouTubeBridgeError,
  }
})

vi.mock('../src/youtube/mpd', () => ({
  buildYouTubeMpd: mocks.buildMpd,
}))

vi.mock('dashjs', () => ({
  MediaPlayer: Object.assign(mocks.mediaPlayer, {
    events: { ERROR: 'dash-error' },
  }),
}))

import YouTubeBrowserPlayer from '../src/components/YouTubeBrowserPlayer'
import { YouTubeBridgeError } from '../src/youtube/bridge'

const ORIGINAL_URL =
  'https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=30s'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function adaptivePlayback(overrides: Partial<BrowserPlayback> = {}): BrowserPlayback {
  return {
    mode: 'dash',
    quality: 1080,
    expiresAt: new Date(Date.now() + 10 * 60_000).toISOString(),
    video: {
      url: 'https://rr1.googlevideo.com/videoplayback?id=video',
      mimeType: 'video/mp4',
      codecs: 'avc1.640028',
      bitrate: 4_000_000,
      initRange: { start: 0, end: 739 },
      indexRange: { start: 740, end: 1251 },
      durationMs: 120_000,
      width: 1920,
      height: 1080,
      frameRate: 30,
    },
    audio: {
      url: 'https://rr1.googlevideo.com/videoplayback?id=audio',
      mimeType: 'audio/mp4',
      codecs: 'mp4a.40.2',
      bitrate: 128_000,
      initRange: { start: 0, end: 721 },
      indexRange: { start: 722, end: 1100 },
      durationMs: 120_000,
      audioSampleRate: 48_000,
    },
    progressive: {
      url: 'https://rr1.googlevideo.com/videoplayback?id=progressive',
      mimeType: 'video/mp4',
      height: 720,
    },
    ...overrides,
  }
}

function progressivePlayback(): BrowserPlayback {
  return {
    mode: 'progressive',
    quality: 720,
    expiresAt: new Date(Date.now() + 10 * 60_000).toISOString(),
    progressive: {
      url: 'https://rr1.googlevideo.com/videoplayback?id=progressive',
      mimeType: 'video/mp4',
      height: 720,
    },
  }
}

function dashWithoutProgressive(): BrowserPlayback {
  const playback = adaptivePlayback()
  delete playback.progressive
  return playback
}

function renderPlayer(start = 30) {
  return render(
    <YouTubeBrowserPlayer
      videoId="dQw4w9WgXcQ"
      start={start}
      originalURL={ORIGINAL_URL}
    />,
  )
}

async function clickStart() {
  fireEvent.click(await screen.findByRole('button', {
    name: '使用已登录的 Chrome 播放',
  }))
}

function setMediaSourceSupported(supported: boolean) {
  Object.defineProperty(window, 'MediaSource', {
    configurable: true,
    value: supported ? class MediaSource {} : undefined,
  })
}

function dashErrorHandler(playerIndex = 0) {
  return mocks.players[playerIndex].on.mock.calls.find(
    ([event]) => event === 'dash-error',
  )?.[1] as (() => void) | undefined
}

describe('YouTubeBrowserPlayer', () => {
  beforeEach(() => {
    mocks.detect.mockReset()
    mocks.resolve.mockReset()
    mocks.buildMpd.mockReset()
    mocks.buildMpd.mockReturnValue('<MPD/>')
    mocks.players.splice(0)
    mocks.create.mockReset()
    mocks.create.mockImplementation(() => {
      const player = {
        initialize: vi.fn(),
        on: vi.fn(),
        off: vi.fn(),
        destroy: vi.fn(),
      }
      mocks.players.push(player)
      return player
    })
    mocks.mediaPlayer.mockReset()
    mocks.mediaPlayer.mockImplementation(() => ({ create: mocks.create }))
    mocks.createObjectURL.mockReset()
    mocks.createObjectURL.mockImplementation(
      () => `blob:rss-pal-${mocks.createObjectURL.mock.calls.length}`,
    )
    mocks.revokeObjectURL.mockReset()
    mocks.load.mockReset()
    mocks.play.mockReset()

    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: mocks.createObjectURL,
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      configurable: true,
      value: mocks.revokeObjectURL,
    })
    Object.defineProperty(HTMLMediaElement.prototype, 'load', {
      configurable: true,
      value: mocks.load,
    })
    Object.defineProperty(HTMLMediaElement.prototype, 'play', {
      configurable: true,
      value: mocks.play,
    })

    setMediaSourceSupported(true)
    mocks.detect.mockResolvedValue({
      available: true,
      version: '1.8.4',
      compatible: true,
    })
  })

  afterEach(() => {
    setMediaSourceSupported(false)
    vi.restoreAllMocks()
  })

  it('detects on mount but does not resolve media before the explicit click', async () => {
    mocks.resolve.mockResolvedValue(progressivePlayback())

    renderPlayer()

    expect(await screen.findByRole('button', {
      name: '使用已登录的 Chrome 播放',
    })).toBeTruthy()
    expect(mocks.detect).toHaveBeenCalledTimes(1)
    expect(mocks.resolve).not.toHaveBeenCalled()
    expect(screen.queryByLabelText('YouTube 视频播放器')).toBeNull()

    await clickStart()

    await waitFor(() => expect(mocks.resolve).toHaveBeenCalledWith(
      'dQw4w9WgXcQ',
      expect.any(AbortSignal),
    ))
    const link = screen.getByRole('link', { name: '在 YouTube 打开' })
    expect(link.getAttribute('href')).toBe(ORIGINAL_URL)
    expect(link.getAttribute('target')).toBe('_blank')
    expect(link.getAttribute('rel')).toContain('noopener')
    expect(link.getAttribute('rel')).toContain('noreferrer')
  })

  it('initializes local DASH at 1080p without autoplay', async () => {
    mocks.resolve.mockResolvedValue(adaptivePlayback())

    renderPlayer()
    await clickStart()

    expect(await screen.findByText('1080p · 本机 Chrome')).toBeTruthy()
    const video = screen.getByLabelText('YouTube 视频播放器')
    const player = mocks.players[0]
    expect(video.getAttribute('controls')).not.toBeNull()
    expect(video.getAttribute('playsinline')).not.toBeNull()
    expect(video.getAttribute('preload')).toBe('metadata')
    expect(video.getAttribute('autoplay')).toBeNull()
    expect(mocks.play).not.toHaveBeenCalled()
    expect(mocks.buildMpd).toHaveBeenCalledWith(
      expect.objectContaining({ quality: 1080 }),
    )
    expect(mocks.createObjectURL).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'application/dash+xml' }),
    )
    expect(player.on).toHaveBeenCalledWith('dash-error', expect.any(Function))
    expect(player.initialize).toHaveBeenCalledWith(
      video,
      'blob:rss-pal-1',
      false,
    )
  })

  it('uses the truthful 720p compatibility fallback without MediaSource', async () => {
    setMediaSourceSupported(false)
    mocks.resolve.mockResolvedValue(adaptivePlayback())

    renderPlayer()
    await clickStart()

    const video = await screen.findByLabelText('YouTube 视频播放器')
    await waitFor(() => expect(video.getAttribute('src')).toContain('id=progressive'))
    expect(screen.getByText('720p · 本机 Chrome · 兼容模式')).toBeTruthy()
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.createObjectURL).not.toHaveBeenCalled()
  })

  it('switches a DASH error to the returned progressive stream', async () => {
    mocks.resolve.mockResolvedValue(adaptivePlayback())

    renderPlayer()
    await clickStart()
    await screen.findByText('1080p · 本机 Chrome')
    dashErrorHandler()?.()

    const video = screen.getByLabelText('YouTube 视频播放器')
    await waitFor(() => expect(video.getAttribute('src')).toContain('id=progressive'))
    expect(screen.getByText('720p · 本机 Chrome · 兼容模式')).toBeTruthy()
    expect(mocks.players[0].off).toHaveBeenCalledWith(
      'dash-error',
      expect.any(Function),
    )
    expect(mocks.players[0].destroy).toHaveBeenCalledTimes(1)
    expect(mocks.revokeObjectURL).toHaveBeenCalledWith('blob:rss-pal-1')
  })

  it('shows the login-required copy with retry and open actions', async () => {
    mocks.resolve
      .mockRejectedValueOnce(new YouTubeBridgeError('LOGIN_REQUIRED'))
      .mockResolvedValueOnce(progressivePlayback())

    renderPlayer()
    await clickStart()

    expect(await screen.findByText('请先在 Chrome 中登录 YouTube')).toBeTruthy()
    expect(screen.getByRole('link', { name: '在 YouTube 打开' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '重试播放' }))

    expect(await screen.findByText('720p · 本机 Chrome · 兼容模式')).toBeTruthy()
    expect(mocks.resolve).toHaveBeenCalledTimes(2)
  })

  it('asks to reload an old extension and never resolves media', async () => {
    mocks.detect.mockResolvedValue({
      available: true,
      version: '1.8.3',
      compatible: false,
    })

    renderPlayer()

    expect(await screen.findByText('请重新加载 RSS Pal 扩展')).toBeTruthy()
    expect(mocks.resolve).not.toHaveBeenCalled()
    expect(screen.queryByLabelText('YouTube 视频播放器')).toBeNull()
  })

  it('reports a missing extension without resolving media', async () => {
    mocks.detect.mockResolvedValue({
      available: false,
      compatible: false,
    })

    renderPlayer()

    expect(await screen.findByText('需要安装并启用 RSS Pal Chrome 扩展')).toBeTruthy()
    expect(mocks.resolve).not.toHaveBeenCalled()
  })

  it('aborts a pending resolve and ignores its late result after unmount', async () => {
    const pending = deferred<BrowserPlayback>()
    mocks.resolve.mockReturnValue(pending.promise)

    const { unmount } = renderPlayer()
    await clickStart()
    await waitFor(() => expect(mocks.resolve).toHaveBeenCalledTimes(1))
    const signal = mocks.resolve.mock.calls[0][1] as AbortSignal

    unmount()
    pending.resolve(adaptivePlayback())
    await Promise.resolve()

    expect(signal.aborted).toBe(true)
    expect(mocks.createObjectURL).not.toHaveBeenCalled()
    expect(mocks.create).not.toHaveBeenCalled()
  })

  it('unregisters, destroys, revokes, and clears an attached DASH player on unmount', async () => {
    mocks.resolve.mockResolvedValue(adaptivePlayback())

    const { unmount } = renderPlayer()
    await clickStart()
    await screen.findByText('1080p · 本机 Chrome')
    const video = screen.getByLabelText('YouTube 视频播放器')

    unmount()

    expect(mocks.players[0].off).toHaveBeenCalledWith(
      'dash-error',
      expect.any(Function),
    )
    expect(mocks.players[0].destroy).toHaveBeenCalledTimes(1)
    expect(mocks.revokeObjectURL).toHaveBeenCalledWith('blob:rss-pal-1')
    expect(video.getAttribute('src')).toBeNull()
    expect(mocks.load).toHaveBeenCalled()
  })

  it('aborts and ignores a pending old identity before requiring a fresh explicit click', async () => {
    const pending = deferred<BrowserPlayback>()
    mocks.resolve
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValueOnce(progressivePlayback())

    const { rerender } = renderPlayer(30)
    await clickStart()
    await waitFor(() => expect(mocks.resolve).toHaveBeenCalledTimes(1))
    const oldSignal = mocks.resolve.mock.calls[0][1] as AbortSignal

    rerender(
      <YouTubeBrowserPlayer
        videoId="M7lc1UVf-VE"
        start={45}
        originalURL="https://www.youtube.com/watch?v=M7lc1UVf-VE&t=45s"
      />,
    )

    await waitFor(() => expect(oldSignal.aborted).toBe(true))
    expect(mocks.detect).toHaveBeenCalledTimes(2)
    expect(await screen.findByRole('button', {
      name: '使用已登录的 Chrome 播放',
    })).toBeTruthy()
    expect(mocks.resolve).toHaveBeenCalledTimes(1)

    pending.resolve(adaptivePlayback())
    await Promise.resolve()
    await Promise.resolve()
    expect(mocks.createObjectURL).not.toHaveBeenCalled()
    expect(mocks.create).not.toHaveBeenCalled()

    await clickStart()
    await waitFor(() => expect(mocks.resolve).toHaveBeenLastCalledWith(
      'M7lc1UVf-VE',
      expect.any(AbortSignal),
    ))
    const video = await screen.findByLabelText('YouTube 视频播放器')
    let currentTime = 0
    const setCurrentTime = vi.fn((value: number) => {
      currentTime = value
    })
    Object.defineProperty(video, 'duration', {
      configurable: true,
      value: 120,
    })
    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      get: () => currentTime,
      set: setCurrentTime,
    })
    fireEvent.loadedMetadata(video)
    expect(setCurrentTime).toHaveBeenCalledWith(45)
    expect(screen.getByRole('link', { name: '在 YouTube 打开' }).getAttribute('href'))
      .toBe('https://www.youtube.com/watch?v=M7lc1UVf-VE&t=45s')
  })

  it('destroys and revokes a ready old identity without auto-resolving the replacement', async () => {
    const player = {
      initialize: vi.fn((video: HTMLVideoElement, manifestURL: string) => {
        video.setAttribute('src', manifestURL)
      }),
      on: vi.fn(),
      off: vi.fn(),
      destroy: vi.fn(),
    }
    mocks.create.mockImplementationOnce(() => {
      mocks.players.push(player)
      return player
    })
    mocks.resolve
      .mockResolvedValueOnce(adaptivePlayback())
      .mockResolvedValueOnce(progressivePlayback())

    const { rerender } = renderPlayer()
    await clickStart()
    await screen.findByText('1080p · 本机 Chrome')
    const oldVideo = screen.getByLabelText('YouTube 视频播放器')
    expect(oldVideo.getAttribute('src')).toBe('blob:rss-pal-1')

    rerender(
      <YouTubeBrowserPlayer
        videoId="M7lc1UVf-VE"
        start={5}
        originalURL="https://www.youtube.com/watch?v=M7lc1UVf-VE&t=5s"
      />,
    )

    expect(await screen.findByRole('button', {
      name: '使用已登录的 Chrome 播放',
    })).toBeTruthy()
    expect(player.off).toHaveBeenCalledWith('dash-error', expect.any(Function))
    expect(player.destroy).toHaveBeenCalledTimes(1)
    expect(mocks.revokeObjectURL).toHaveBeenCalledWith('blob:rss-pal-1')
    expect(oldVideo.getAttribute('src')).toBeNull()
    expect(mocks.resolve).toHaveBeenCalledTimes(1)
    expect(mocks.detect).toHaveBeenCalledTimes(2)
  })

  it('turns a synchronous DASH initialize error into one automatic retry', async () => {
    const noFallback = dashWithoutProgressive()
    let synchronousError: (() => void) | undefined
    const player = {
      initialize: vi.fn(() => synchronousError?.()),
      on: vi.fn((_event: string, handler: () => void) => {
        synchronousError = handler
      }),
      off: vi.fn(),
      destroy: vi.fn(),
    }
    mocks.create.mockImplementationOnce(() => {
      mocks.players.push(player)
      return player
    })
    mocks.resolve
      .mockResolvedValueOnce(noFallback)
      .mockRejectedValueOnce(new YouTubeBridgeError('VIDEO_UNAVAILABLE'))

    renderPlayer()
    await clickStart()

    await waitFor(() => expect(mocks.resolve).toHaveBeenCalledTimes(2))
    expect(await screen.findByText('视频暂时无法加载')).toBeTruthy()
    expect(screen.queryByText('1080p · 本机 Chrome')).toBeNull()
    expect(mocks.resolve).toHaveBeenCalledTimes(2)
    expect(player.off).toHaveBeenCalledWith(
      'dash-error',
      expect.any(Function),
    )
    expect(player.destroy).toHaveBeenCalledTimes(1)
    expect(mocks.revokeObjectURL).toHaveBeenCalledWith('blob:rss-pal-1')
  })

  it('uses the progressive fallback for a synchronous DASH initialize error', async () => {
    let synchronousError: (() => void) | undefined
    const player = {
      initialize: vi.fn(() => synchronousError?.()),
      on: vi.fn((_event: string, handler: () => void) => {
        synchronousError = handler
      }),
      off: vi.fn(),
      destroy: vi.fn(),
    }
    mocks.create.mockImplementationOnce(() => {
      mocks.players.push(player)
      return player
    })
    mocks.resolve.mockResolvedValueOnce(adaptivePlayback())

    renderPlayer()
    await clickStart()

    const video = await screen.findByLabelText('YouTube 视频播放器')
    await waitFor(() => expect(video.getAttribute('src')).toContain('id=progressive'))
    expect(screen.getByText('720p · 本机 Chrome · 兼容模式')).toBeTruthy()
    expect(mocks.resolve).toHaveBeenCalledTimes(1)
    expect(player.destroy).toHaveBeenCalledTimes(1)
    expect(mocks.revokeObjectURL).toHaveBeenCalledWith('blob:rss-pal-1')
  })

  it('automatically re-resolves a native failure once, then shows a visible error', async () => {
    const noFallback = dashWithoutProgressive()
    mocks.resolve.mockResolvedValue(noFallback)

    renderPlayer()
    await clickStart()
    const video = await screen.findByLabelText('YouTube 视频播放器')
    await screen.findByText('1080p · 本机 Chrome')

    fireEvent.error(video)
    await waitFor(() => expect(mocks.resolve).toHaveBeenCalledTimes(2))
    await screen.findByText('1080p · 本机 Chrome')
    fireEvent.error(video)

    expect(await screen.findByText('视频暂时无法加载')).toBeTruthy()
    expect(mocks.resolve).toHaveBeenCalledTimes(2)
    expect(screen.getByRole('button', { name: '重试播放' })).toBeTruthy()
  })

  it('rejects playback that expires in less than thirty seconds', async () => {
    mocks.resolve.mockResolvedValue(progressivePlayback())
    mocks.resolve.mockResolvedValueOnce({
      ...progressivePlayback(),
      expiresAt: new Date(Date.now() + 29_000).toISOString(),
    })

    renderPlayer()
    await clickStart()

    expect(await screen.findByText('视频暂时无法加载')).toBeTruthy()
    expect(mocks.createObjectURL).not.toHaveBeenCalled()
    expect(screen.queryByText(/googlevideo/)).toBeNull()
  })

  it('deduplicates repeated explicit clicks while one resolve is pending', async () => {
    const pending = deferred<BrowserPlayback>()
    mocks.resolve.mockReturnValue(pending.promise)

    renderPlayer()
    const button = await screen.findByRole('button', {
      name: '使用已登录的 Chrome 播放',
    })
    fireEvent.click(button)
    fireEvent.click(button)

    expect(mocks.resolve).toHaveBeenCalledTimes(1)
    pending.resolve(progressivePlayback())
    expect(await screen.findByText('720p · 本机 Chrome · 兼容模式')).toBeTruthy()
  })

  it('falls back when dash.js initialization fails', async () => {
    mocks.resolve.mockResolvedValue(adaptivePlayback())
    mocks.create.mockImplementationOnce(() => {
      const player = {
        initialize: vi.fn(() => {
          throw new Error('dash init leaked signed URL https://rr1.googlevideo.com')
        }),
        on: vi.fn(),
        off: vi.fn(),
        destroy: vi.fn(),
      }
      mocks.players.push(player)
      return player
    })

    renderPlayer()
    await clickStart()

    const video = await screen.findByLabelText('YouTube 视频播放器')
    await waitFor(() => expect(video.getAttribute('src')).toContain('id=progressive'))
    expect(screen.getByText('720p · 本机 Chrome · 兼容模式')).toBeTruthy()
    expect(mocks.players[0].off).toHaveBeenCalled()
    expect(mocks.players[0].destroy).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/googlevideo|dash init leaked/)).toBeNull()
  })

  it('applies an in-range positive start once after metadata loads', async () => {
    mocks.resolve.mockResolvedValue(progressivePlayback())
    renderPlayer(30)
    await clickStart()
    const video = await screen.findByLabelText('YouTube 视频播放器')
    let currentTime = 0
    const setCurrentTime = vi.fn((value: number) => {
      currentTime = value
    })
    Object.defineProperty(video, 'duration', {
      configurable: true,
      value: 120,
    })
    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      get: () => currentTime,
      set: setCurrentTime,
    })

    fireEvent.loadedMetadata(video)
    fireEvent.loadedMetadata(video)

    expect(setCurrentTime).toHaveBeenCalledTimes(1)
    expect(setCurrentTime).toHaveBeenCalledWith(30)
  })

  it.each([0, 120, 121])(
    'does not apply an out-of-range start of %s seconds',
    async start => {
      mocks.resolve.mockResolvedValue(progressivePlayback())
      renderPlayer(start)
      await clickStart()
      const video = await screen.findByLabelText('YouTube 视频播放器')
      const setCurrentTime = vi.fn()
      Object.defineProperty(video, 'duration', {
        configurable: true,
        value: 120,
      })
      Object.defineProperty(video, 'currentTime', {
        configurable: true,
        get: () => 0,
        set: setCurrentTime,
      })

      fireEvent.loadedMetadata(video)

      expect(setCurrentTime).not.toHaveBeenCalled()
    },
  )

  it('maps local network and unknown failures without exposing raw details', async () => {
    mocks.resolve
      .mockRejectedValueOnce(new YouTubeBridgeError('LOCAL_NETWORK_ERROR'))
      .mockRejectedValueOnce(
        new Error(
          'secret https://rr1.googlevideo.com/videoplayback?signature=raw',
        ),
      )

    renderPlayer()
    await clickStart()

    expect(await screen.findByText('本机无法连接 YouTube，请检查 Clash')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '重试播放' }))
    await waitFor(() => expect(mocks.resolve).toHaveBeenCalledTimes(2))
    expect(await screen.findByText('视频暂时无法加载')).toBeTruthy()
    expect(screen.queryByText(/secret|googlevideo|signature/)).toBeNull()
  })

  it('resets the automatic media-retry guard after a visible retry', async () => {
    const noFallback = dashWithoutProgressive()
    mocks.resolve.mockResolvedValue(noFallback)

    renderPlayer()
    await clickStart()
    const video = await screen.findByLabelText('YouTube 视频播放器')
    await screen.findByText('1080p · 本机 Chrome')
    fireEvent.error(video)
    await waitFor(() => expect(mocks.resolve).toHaveBeenCalledTimes(2))
    await screen.findByText('1080p · 本机 Chrome')
    fireEvent.error(video)
    fireEvent.click(await screen.findByRole('button', { name: '重试播放' }))
    await waitFor(() => expect(mocks.resolve).toHaveBeenCalledTimes(3))
    await screen.findByText('1080p · 本机 Chrome')
    fireEvent.error(video)

    await waitFor(() => expect(mocks.resolve).toHaveBeenCalledTimes(4))
  })

  it('ignores a stale DASH error callback after a newer player is attached', async () => {
    const noFallback = dashWithoutProgressive()
    mocks.resolve.mockResolvedValue(noFallback)

    renderPlayer()
    await clickStart()
    const video = await screen.findByLabelText('YouTube 视频播放器')
    await screen.findByText('1080p · 本机 Chrome')
    const staleError = dashErrorHandler(0)
    fireEvent.error(video)
    await waitFor(() => expect(mocks.players).toHaveLength(2))
    await screen.findByText('1080p · 本机 Chrome')

    staleError?.()
    await Promise.resolve()

    expect(mocks.resolve).toHaveBeenCalledTimes(2)
    expect(mocks.players[1].destroy).not.toHaveBeenCalled()
    expect(screen.getByText('1080p · 本机 Chrome')).toBeTruthy()
  })
})
