import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  MIN_YOUTUBE_BRIDGE_VERSION,
  YouTubeBridgeError,
  detectYouTubeBridge,
  resolveYouTubePlayback,
  type BrowserPlayback,
} from '../src/youtube/bridge'

const ORIGIN = window.location.origin
const VIDEO_ID = 'dQw4w9WgXcQ'

function googleVideoURL(itag: number): string {
  return `https://rr1---sn-a5mekn6z.googlevideo.com/videoplayback?itag=${itag}&sig=test`
}

function futureExpiry(): string {
  return new Date(Date.now() + 10 * 60_000).toISOString()
}

function playbackFixture(): BrowserPlayback {
  return {
    mode: 'dash',
    quality: 1080,
    expiresAt: futureExpiry(),
    video: {
      url: googleVideoURL(137),
      mimeType: 'video/mp4',
      codecs: 'avc1.640028',
      bitrate: 3_500_000,
      initRange: { start: 0, end: 739 },
      indexRange: { start: 740, end: 1_200 },
      durationMs: 212_000,
      width: 1920,
      height: 1080,
      frameRate: 30,
    },
    audio: {
      url: googleVideoURL(140),
      mimeType: 'audio/mp4',
      codecs: 'mp4a.40.2',
      bitrate: 128_000,
      initRange: { start: 0, end: 719 },
      indexRange: { start: 720, end: 1_100 },
      durationMs: 212_000,
      audioSampleRate: 44_100,
    },
  }
}

function clonePlayback(): BrowserPlayback {
  return JSON.parse(JSON.stringify(playbackFixture())) as BrowserPlayback
}

function dispatchBridgeMessage(
  data: unknown,
  options: { origin?: string; source?: MessageEventSource | null } = {},
): void {
  window.dispatchEvent(
    new MessageEvent('message', {
      data,
      origin: options.origin ?? ORIGIN,
      source: options.source === undefined ? window : options.source,
    }),
  )
}

function answerPingWith(version: string): () => void {
  const listener = (event: MessageEvent) => {
    if (event.data?.type !== 'RSS_PAL_YOUTUBE_BRIDGE_PING') return
    dispatchBridgeMessage({
      type: 'RSS_PAL_YOUTUBE_BRIDGE_READY',
      version,
    })
  }
  window.addEventListener('message', listener)
  return () => window.removeEventListener('message', listener)
}

function answerResolveWith(
  response:
    | { ok: true; playback: unknown }
    | { ok: false; code: unknown },
  beforeResponse?: (request: { requestId: string; videoId: string }) => void,
): () => void {
  const listener = (event: MessageEvent) => {
    if (event.data?.type !== 'RSS_PAL_YOUTUBE_RESOLVE_REQUEST') return
    const request = event.data as { requestId: string; videoId: string }
    beforeResponse?.(request)
    dispatchBridgeMessage({
      type: 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE',
      requestId: request.requestId,
      ...response,
    })
  }
  window.addEventListener('message', listener)
  return () => window.removeEventListener('message', listener)
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('detectYouTubeBridge', () => {
  it('detects extension 1.8.4 through a same-window, same-origin ping', async () => {
    const cleanup = answerPingWith('1.8.4')

    await expect(detectYouTubeBridge(100)).resolves.toEqual({
      available: true,
      version: '1.8.4',
      compatible: true,
    })
    expect(MIN_YOUTUBE_BRIDGE_VERSION).toBe('1.8.4')
    cleanup()
  })

  it('marks an older extension as incompatible', async () => {
    const cleanup = answerPingWith('1.8.3')

    await expect(detectYouTubeBridge(100)).resolves.toEqual({
      available: true,
      version: '1.8.3',
      compatible: false,
    })
    cleanup()
  })

  it('uses exact numeric versions instead of accepting suffixed versions', async () => {
    const cleanup = answerPingWith('1.8.4-beta')

    await expect(detectYouTubeBridge(100)).resolves.toEqual({
      available: true,
      version: '1.8.4-beta',
      compatible: false,
    })
    cleanup()
  })

  it('ignores ready messages from a foreign source or origin', async () => {
    const listener = (event: MessageEvent) => {
      if (event.data?.type !== 'RSS_PAL_YOUTUBE_BRIDGE_PING') return
      dispatchBridgeMessage(
        { type: 'RSS_PAL_YOUTUBE_BRIDGE_READY', version: '99.0.0' },
        { origin: 'https://evil.example' },
      )
      dispatchBridgeMessage(
        { type: 'RSS_PAL_YOUTUBE_BRIDGE_READY', version: '99.0.0' },
        { source: null },
      )
      dispatchBridgeMessage({
        type: 'RSS_PAL_YOUTUBE_BRIDGE_READY',
        version: '1.8.4',
      })
    }
    window.addEventListener('message', listener)

    await expect(detectYouTubeBridge(100)).resolves.toEqual({
      available: true,
      version: '1.8.4',
      compatible: true,
    })
    window.removeEventListener('message', listener)
  })

  it('returns unavailable after the detection timeout', async () => {
    await expect(detectYouTubeBridge(10)).resolves.toEqual({
      available: false,
      compatible: false,
    })
  })
})

describe('resolveYouTubePlayback', () => {
  it('resolves only the matching request and validates GoogleVideo URLs', async () => {
    const playback = playbackFixture()
    let postedRequestId = ''
    const cleanup = answerResolveWith(
      { ok: true, playback },
      request => {
        postedRequestId = request.requestId
      },
    )

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).resolves.toEqual(playback)
    expect(postedRequestId).toMatch(/^[A-Za-z0-9_]+$/)
    expect(postedRequestId).not.toContain('-')
    cleanup()
  })

  it('ignores a response for a mismatched request ID', async () => {
    const playback = playbackFixture()
    const listener = (event: MessageEvent) => {
      if (event.data?.type !== 'RSS_PAL_YOUTUBE_RESOLVE_REQUEST') return
      dispatchBridgeMessage({
        type: 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE',
        requestId: 'another_request',
        ok: true,
        playback,
      })
      dispatchBridgeMessage({
        type: 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE',
        requestId: event.data.requestId,
        ok: true,
        playback,
      })
    }
    window.addEventListener('message', listener)

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).resolves.toEqual(playback)
    window.removeEventListener('message', listener)
  })

  it('ignores responses from a foreign source or origin', async () => {
    const playback = playbackFixture()
    const listener = (event: MessageEvent) => {
      if (event.data?.type !== 'RSS_PAL_YOUTUBE_RESOLVE_REQUEST') return
      const response = {
        type: 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE',
        requestId: event.data.requestId,
        ok: true,
        playback,
      }
      dispatchBridgeMessage(response, { origin: 'https://evil.example' })
      dispatchBridgeMessage(response, { source: null })
      dispatchBridgeMessage(response)
    }
    window.addEventListener('message', listener)

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).resolves.toEqual(playback)
    window.removeEventListener('message', listener)
  })

  it('rejects an invalid video ID before posting a request', async () => {
    const posted: unknown[] = []
    const listener = (event: MessageEvent) => posted.push(event.data)
    window.addEventListener('message', listener)

    await expect(
      resolveYouTubePlayback('not a video', undefined, 100),
    ).rejects.toMatchObject({
      name: 'YouTubeBridgeError',
      code: 'INTERNAL_ERROR',
    })
    expect(posted).not.toContainEqual(
      expect.objectContaining({
        type: 'RSS_PAL_YOUTUBE_RESOLVE_REQUEST',
      }),
    )
    window.removeEventListener('message', listener)
  })

  it('rejects a non-GoogleVideo media URL as an internal error', async () => {
    const playback = clonePlayback()
    playback.video!.url =
      'https://googlevideo.com.evil.example/videoplayback?itag=137'
    const cleanup = answerResolveWith({ ok: true, playback })

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).rejects.toMatchObject({
      name: 'YouTubeBridgeError',
      code: 'INTERNAL_ERROR',
    })
    cleanup()
  })

  it.each([
    ['NaN bitrate', (value: BrowserPlayback) => {
      value.video!.bitrate = Number.NaN
    }],
    ['infinite duration', (value: BrowserPlayback) => {
      value.audio!.durationMs = Number.POSITIVE_INFINITY
    }],
    ['zero quality', (value: BrowserPlayback) => {
      value.quality = 0
    }],
    ['unbounded frame rate', (value: BrowserPlayback) => {
      value.video!.frameRate = 100_000
    }],
    ['fractional range', (value: BrowserPlayback) => {
      value.video!.initRange.start = 0.5
    }],
    ['negative range', (value: BrowserPlayback) => {
      value.audio!.indexRange.start = -1
    }],
    ['reversed range', (value: BrowserPlayback) => {
      value.video!.indexRange = { start: 20, end: 10 }
    }],
  ])('rejects malformed numeric metadata: %s', async (_name, mutate) => {
    const playback = clonePlayback()
    mutate(playback)
    const cleanup = answerResolveWith({ ok: true, playback })

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).rejects.toMatchObject({ code: 'INTERNAL_ERROR' })
    cleanup()
  })

  it.each([
    ['an invalid date', 'not-a-date'],
    ['an expired date', new Date(Date.now() - 1_000).toISOString()],
  ])('rejects %s as the signed URL expiry', async (_name, expiresAt) => {
    const playback = clonePlayback()
    playback.expiresAt = expiresAt
    const cleanup = answerResolveWith({ ok: true, playback })

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).rejects.toMatchObject({ code: 'INTERNAL_ERROR' })
    cleanup()
  })

  it('requires exactly the adaptive tracks for DASH mode', async () => {
    const playback = clonePlayback() as BrowserPlayback & {
      debug?: string
    }
    delete playback.audio
    playback.debug = 'must not cross the boundary'
    const cleanup = answerResolveWith({ ok: true, playback })

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).rejects.toMatchObject({ code: 'INTERNAL_ERROR' })
    cleanup()
  })

  it('requires exactly a progressive track for progressive mode', async () => {
    const playback = {
      mode: 'progressive',
      quality: 720,
      expiresAt: futureExpiry(),
      video: clonePlayback().video,
    }
    const cleanup = answerResolveWith({ ok: true, playback })

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).rejects.toMatchObject({ code: 'INTERNAL_ERROR' })
    cleanup()
  })

  it('accepts a valid exact progressive playback envelope', async () => {
    const playback: BrowserPlayback = {
      mode: 'progressive',
      quality: 720,
      expiresAt: futureExpiry(),
      progressive: {
        url: googleVideoURL(22),
        mimeType: 'video/mp4',
        height: 720,
      },
    }
    const cleanup = answerResolveWith({ ok: true, playback })

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).resolves.toEqual(playback)
    cleanup()
  })

  it('maps allowlisted extension failures to YouTubeBridgeError', async () => {
    const cleanup = answerResolveWith({
      ok: false,
      code: 'LOGIN_REQUIRED',
    })

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).rejects.toEqual(new YouTubeBridgeError('LOGIN_REQUIRED'))
    cleanup()
  })

  it('sanitizes unknown extension failures to INTERNAL_ERROR', async () => {
    const cleanup = answerResolveWith({
      ok: false,
      code: 'ACCOUNT_SECRET',
    })

    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 100),
    ).rejects.toEqual(new YouTubeBridgeError('INTERNAL_ERROR'))
    cleanup()
  })

  it('maps a response timeout to EXTENSION_UNAVAILABLE', async () => {
    await expect(
      resolveYouTubePlayback(VIDEO_ID, undefined, 10),
    ).rejects.toEqual(new YouTubeBridgeError('EXTENSION_UNAVAILABLE'))
  })

  it('posts cancellation, cleans up, and ignores late settlement when aborted', async () => {
    const controller = new AbortController()
    const posted: unknown[] = []
    let requestId = ''
    const listener = (event: MessageEvent) => {
      posted.push(event.data)
      if (event.data?.type === 'RSS_PAL_YOUTUBE_RESOLVE_REQUEST') {
        requestId = event.data.requestId
      }
    }
    const removeSpy = vi.spyOn(window, 'removeEventListener')
    const clearTimerSpy = vi.spyOn(window, 'clearTimeout')
    window.addEventListener('message', listener)

    const pending = resolveYouTubePlayback(VIDEO_ID, controller.signal, 1_000)
    await vi.waitFor(() => expect(requestId).not.toBe(''))
    controller.abort()
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    await vi.waitFor(() =>
      expect(posted).toContainEqual({
        type: 'RSS_PAL_YOUTUBE_RESOLVE_CANCEL',
        requestId,
      }),
    )
    expect(removeSpy).toHaveBeenCalledWith('message', expect.any(Function))
    expect(clearTimerSpy).toHaveBeenCalled()

    dispatchBridgeMessage({
      type: 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE',
      requestId,
      ok: true,
      playback: playbackFixture(),
    })
    await Promise.resolve()
    window.removeEventListener('message', listener)
  })
})
