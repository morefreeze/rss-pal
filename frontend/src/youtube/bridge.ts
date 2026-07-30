export type ByteRange = {
  start: number
  end: number
}

export type AdaptiveTrack = {
  url: string
  mimeType: string
  codecs: string
  bitrate: number
  initRange: ByteRange
  indexRange: ByteRange
  durationMs: number
  width?: number
  height?: number
  frameRate?: number
  audioSampleRate?: number
}

export type ProgressiveTrack = {
  url: string
  mimeType: string
  height: number
}

export type BrowserPlayback = {
  mode: 'dash' | 'progressive'
  quality: number
  expiresAt: string
  video?: AdaptiveTrack
  audio?: AdaptiveTrack
  progressive?: ProgressiveTrack
}

export const MIN_YOUTUBE_BRIDGE_VERSION = '1.8.4'

export type YouTubeBridgeErrorCode =
  | 'EXTENSION_UNAVAILABLE'
  | 'LOGIN_REQUIRED'
  | 'VIDEO_UNAVAILABLE'
  | 'NO_SUPPORTED_FORMAT'
  | 'RESOLVE_TIMEOUT'
  | 'LOCAL_NETWORK_ERROR'
  | 'PLAYBACK_EXPIRED'
  | 'INTERNAL_ERROR'

export class YouTubeBridgeError extends Error {
  constructor(public readonly code: YouTubeBridgeErrorCode) {
    super(code)
    this.name = 'YouTubeBridgeError'
  }
}

const BRIDGE_PING = 'RSS_PAL_YOUTUBE_BRIDGE_PING'
const BRIDGE_READY = 'RSS_PAL_YOUTUBE_BRIDGE_READY'
const RESOLVE_REQUEST = 'RSS_PAL_YOUTUBE_RESOLVE_REQUEST'
const RESOLVE_CANCEL = 'RSS_PAL_YOUTUBE_RESOLVE_CANCEL'
const RESOLVE_RESPONSE = 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE'

const VIDEO_ID_RE = /^[A-Za-z0-9_-]{11}$/
const VERSION_RE = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/

const ERROR_CODES = new Set<YouTubeBridgeErrorCode>([
  'EXTENSION_UNAVAILABLE',
  'LOGIN_REQUIRED',
  'VIDEO_UNAVAILABLE',
  'NO_SUPPORTED_FORMAT',
  'RESOLVE_TIMEOUT',
  'LOCAL_NETWORK_ERROR',
  'PLAYBACK_EXPIRED',
  'INTERNAL_ERROR',
])

const MAX_URL_LENGTH = 16_384
const MAX_MIME_LENGTH = 128
const MAX_CODECS_LENGTH = 256
const MAX_QUALITY = 4_320
const MAX_DIMENSION = 16_384
const MAX_BITRATE = 2_000_000_000
const MAX_DURATION_MS = 7 * 24 * 60 * 60 * 1_000
const MAX_FRAME_RATE = 240
const MAX_AUDIO_SAMPLE_RATE = 768_000
const MAX_MEDIA_RANGE_OFFSET = 64 * 1024 * 1024 - 1
const MAX_MEDIA_RANGE_SPAN = 16 * 1024 * 1024

type UnknownRecord = Record<PropertyKey, unknown>

function isRecord(value: unknown): value is UnknownRecord {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function hasExactKeys(
  value: UnknownRecord,
  required: readonly string[],
  optional: readonly string[] = [],
): boolean {
  const keys = Reflect.ownKeys(value)
  const allowed = new Set([...required, ...optional])
  return (
    keys.length >= required.length &&
    required.every(key => keys.includes(key)) &&
    keys.every(key => typeof key === 'string' && allowed.has(key))
  )
}

function parseVersion(value: string): [number, number, number] | null {
  const match = VERSION_RE.exec(value)
  if (match === null) return null

  const version = match.slice(1).map(Number)
  if (!version.every(Number.isSafeInteger)) return null
  return version as [number, number, number]
}

function compareVersions(left: string, right: string): number | null {
  const a = parseVersion(left)
  const b = parseVersion(right)
  if (a === null || b === null) return null

  for (let index = 0; index < a.length; index += 1) {
    const difference = a[index] - b[index]
    if (difference !== 0) return difference
  }
  return 0
}

function isBoundedInteger(
  value: unknown,
  maximum: number,
  minimum = 1,
): value is number {
  return (
    typeof value === 'number' &&
    Number.isSafeInteger(value) &&
    value >= minimum &&
    value <= maximum
  )
}

function isBoundedString(value: unknown, maximum: number): value is string {
  return typeof value === 'string' && value.length > 0 && value.length <= maximum
}

function validMediaURL(rawURL: unknown): rawURL is string {
  if (
    typeof rawURL !== 'string' ||
    rawURL.length === 0 ||
    rawURL.length > MAX_URL_LENGTH
  ) {
    return false
  }

  try {
    const url = new URL(rawURL)
    const trustedHost =
      url.hostname === 'googlevideo.com' ||
      url.hostname.endsWith('.googlevideo.com')
    const trustedPath =
      url.pathname === '/videoplayback' ||
      url.pathname.startsWith('/videoplayback/')

    return (
      url.protocol === 'https:' &&
      trustedHost &&
      trustedPath &&
      url.username === '' &&
      url.password === ''
    )
  } catch {
    return false
  }
}

function validByteRange(value: unknown): value is ByteRange {
  return (
    isRecord(value) &&
    hasExactKeys(value, ['start', 'end']) &&
    isBoundedInteger(value.start, MAX_MEDIA_RANGE_OFFSET, 0) &&
    isBoundedInteger(value.end, MAX_MEDIA_RANGE_OFFSET, 0) &&
    value.end >= value.start &&
    value.end - value.start + 1 <= MAX_MEDIA_RANGE_SPAN
  )
}

const ADAPTIVE_REQUIRED_KEYS = [
  'url',
  'mimeType',
  'codecs',
  'bitrate',
  'initRange',
  'indexRange',
  'durationMs',
] as const

const ADAPTIVE_OPTIONAL_KEYS = [
  'width',
  'height',
  'frameRate',
  'audioSampleRate',
] as const

function validAdaptiveTrack(
  value: unknown,
  mediaType: 'video' | 'audio',
): value is AdaptiveTrack {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ADAPTIVE_REQUIRED_KEYS, ADAPTIVE_OPTIONAL_KEYS) ||
    !validMediaURL(value.url) ||
    !isBoundedString(value.mimeType, MAX_MIME_LENGTH) ||
    !value.mimeType.toLowerCase().startsWith(`${mediaType}/`) ||
    !isBoundedString(value.codecs, MAX_CODECS_LENGTH) ||
    !isBoundedInteger(value.bitrate, MAX_BITRATE) ||
    !validByteRange(value.initRange) ||
    !validByteRange(value.indexRange) ||
    !isBoundedInteger(value.durationMs, MAX_DURATION_MS)
  ) {
    return false
  }

  const optionalNumbers = [
    ['width', MAX_DIMENSION],
    ['height', MAX_DIMENSION],
    ['frameRate', MAX_FRAME_RATE],
    ['audioSampleRate', MAX_AUDIO_SAMPLE_RATE],
  ] as const

  return optionalNumbers.every(
    ([key, maximum]) =>
      value[key] === undefined || isBoundedInteger(value[key], maximum),
  )
}

function validProgressiveTrack(value: unknown): value is ProgressiveTrack {
  return (
    isRecord(value) &&
    hasExactKeys(value, ['url', 'mimeType', 'height']) &&
    validMediaURL(value.url) &&
    isBoundedString(value.mimeType, MAX_MIME_LENGTH) &&
    value.mimeType.toLowerCase().startsWith('video/') &&
    isBoundedInteger(value.height, MAX_DIMENSION)
  )
}

function validFutureExpiry(value: unknown): value is string {
  if (typeof value !== 'string' || value.length === 0 || value.length > 64) {
    return false
  }
  const expiresAt = Date.parse(value)
  return Number.isFinite(expiresAt) && expiresAt > Date.now()
}

function validPlayback(value: unknown): value is BrowserPlayback {
  if (
    !isRecord(value) ||
    !isBoundedInteger(value.quality, MAX_QUALITY) ||
    !validFutureExpiry(value.expiresAt)
  ) {
    return false
  }

  if (value.mode === 'dash') {
    return (
      hasExactKeys(value, ['mode', 'quality', 'expiresAt', 'video', 'audio']) &&
      validAdaptiveTrack(value.video, 'video') &&
      validAdaptiveTrack(value.audio, 'audio')
    )
  }

  if (value.mode === 'progressive') {
    return (
      hasExactKeys(value, [
        'mode',
        'quality',
        'expiresAt',
        'progressive',
      ]) && validProgressiveTrack(value.progressive)
    )
  }

  return false
}

function isBridgeErrorCode(value: unknown): value is YouTubeBridgeErrorCode {
  return typeof value === 'string' && ERROR_CODES.has(value as YouTubeBridgeErrorCode)
}

export async function detectYouTubeBridge(timeoutMs = 600): Promise<{
  available: boolean
  version?: string
  compatible: boolean
}> {
  return new Promise(resolve => {
    let settled = false

    const cleanup = () => {
      window.clearTimeout(timer)
      window.removeEventListener('message', onMessage)
    }

    const finish = (result: {
      available: boolean
      version?: string
      compatible: boolean
    }) => {
      if (settled) return
      settled = true
      cleanup()
      resolve(result)
    }

    function onMessage(event: MessageEvent) {
      if (
        event.source !== window ||
        event.origin !== window.location.origin ||
        !isRecord(event.data) ||
        !hasExactKeys(event.data, ['type', 'version']) ||
        event.data.type !== BRIDGE_READY ||
        typeof event.data.version !== 'string'
      ) {
        return
      }

      const comparison = compareVersions(
        event.data.version,
        MIN_YOUTUBE_BRIDGE_VERSION,
      )
      finish({
        available: true,
        version: event.data.version,
        compatible: comparison !== null && comparison >= 0,
      })
    }

    const timer = window.setTimeout(
      () => finish({ available: false, compatible: false }),
      timeoutMs,
    )
    window.addEventListener('message', onMessage)

    try {
      window.postMessage({ type: BRIDGE_PING }, window.location.origin)
    } catch {
      finish({ available: false, compatible: false })
    }
  })
}

export async function resolveYouTubePlayback(
  videoId: string,
  signal?: AbortSignal,
  timeoutMs = 50_000,
): Promise<BrowserPlayback> {
  if (!VIDEO_ID_RE.test(videoId)) {
    throw new YouTubeBridgeError('INTERNAL_ERROR')
  }

  const requestId = crypto.randomUUID().split('-').join('_')

  return new Promise((resolve, reject) => {
    let settled = false
    let cancelPosted = false

    const cleanup = () => {
      window.clearTimeout(timer)
      window.removeEventListener('message', onMessage)
      signal?.removeEventListener('abort', onAbort)
    }

    const finishResolve = (playback: BrowserPlayback) => {
      if (settled) return
      settled = true
      cleanup()
      resolve(playback)
    }

    const finishReject = (error: YouTubeBridgeError | DOMException) => {
      if (settled) return
      settled = true
      cleanup()
      reject(error)
    }

    const postCancel = () => {
      if (settled || cancelPosted) return
      cancelPosted = true
      try {
        window.postMessage(
          {
            type: RESOLVE_CANCEL,
            requestId,
          },
          window.location.origin,
        )
      } catch {
        // Cancellation is best-effort.
      }
    }

    function onMessage(event: MessageEvent) {
      if (
        event.source !== window ||
        event.origin !== window.location.origin ||
        !isRecord(event.data) ||
        event.data.type !== RESOLVE_RESPONSE ||
        event.data.requestId !== requestId
      ) {
        return
      }

      if (
        hasExactKeys(event.data, [
          'type',
          'requestId',
          'ok',
          'playback',
        ]) &&
        event.data.ok === true
      ) {
        if (validPlayback(event.data.playback)) {
          finishResolve(event.data.playback)
        } else {
          finishReject(new YouTubeBridgeError('INTERNAL_ERROR'))
        }
        return
      }

      if (
        hasExactKeys(event.data, ['type', 'requestId', 'ok', 'code']) &&
        event.data.ok === false
      ) {
        finishReject(
          new YouTubeBridgeError(
            isBridgeErrorCode(event.data.code)
              ? event.data.code
              : 'INTERNAL_ERROR',
          ),
        )
        return
      }

      finishReject(new YouTubeBridgeError('INTERNAL_ERROR'))
    }

    function onAbort() {
      if (settled) return
      postCancel()
      finishReject(new DOMException('The operation was aborted.', 'AbortError'))
    }

    const timer = window.setTimeout(
      () => {
        postCancel()
        finishReject(new YouTubeBridgeError('EXTENSION_UNAVAILABLE'))
      },
      timeoutMs,
    )
    window.addEventListener('message', onMessage)
    signal?.addEventListener('abort', onAbort, { once: true })

    if (signal?.aborted) {
      onAbort()
      return
    }

    try {
      window.postMessage(
        {
          type: RESOLVE_REQUEST,
          requestId,
          videoId,
        },
        window.location.origin,
      )
    } catch {
      finishReject(new YouTubeBridgeError('INTERNAL_ERROR'))
    }
  })
}
