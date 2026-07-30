import type { AdaptiveTrack, BrowserPlayback, ByteRange } from './bridge'

const INVALID_XML_CHARACTER =
  /[^\u0009\u000A\u000D\u0020-\uD7FF\uE000-\uFFFD\u{10000}-\u{10FFFF}]/u

function xml(value: string | number): string {
  const text = String(value)
  if (INVALID_XML_CHARACTER.test(text)) {
    throw new Error('invalid XML character')
  }

  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;')
}

function requiredString(value: unknown, name: string): string {
  if (typeof value !== 'string' || value.length === 0) {
    throw new Error(`${name} must be a non-empty string`)
  }
  return value
}

function positiveInteger(value: unknown, name: string): number {
  if (
    typeof value !== 'number' ||
    !Number.isSafeInteger(value) ||
    value <= 0
  ) {
    throw new Error(`${name} must be a positive integer`)
  }
  return value
}

function optionalPositiveInteger(value: unknown, name: string): number | undefined {
  if (value === undefined) return undefined
  return positiveInteger(value, name)
}

function range(value: unknown, name: string): string {
  if (value === null || typeof value !== 'object') {
    throw new Error(`${name} must be a byte range`)
  }

  const byteRange = value as Partial<ByteRange>
  if (
    !Number.isSafeInteger(byteRange.start) ||
    !Number.isSafeInteger(byteRange.end) ||
    (byteRange.start as number) < 0 ||
    (byteRange.end as number) < (byteRange.start as number)
  ) {
    throw new Error(`${name} must be a valid byte range`)
  }

  return `${xml(byteRange.start as number)}-${xml(byteRange.end as number)}`
}

function representation(
  track: AdaptiveTrack,
  kind: 'video' | 'audio',
): string {
  if (track === null || typeof track !== 'object') {
    throw new Error(`${kind} track is required`)
  }

  const url = requiredString(track.url, `${kind} URL`)
  const mimeType = requiredString(track.mimeType, `${kind} MIME type`)
  const codecs = requiredString(track.codecs, `${kind} codecs`)
  const bitrate = positiveInteger(track.bitrate, `${kind} bitrate`)

  let mediaAttributes = ''
  if (kind === 'video') {
    const width = optionalPositiveInteger(track.width, 'video width')
    const height = optionalPositiveInteger(track.height, 'video height')
    const frameRate = optionalPositiveInteger(track.frameRate, 'video frame rate')
    if (width !== undefined) mediaAttributes += ` width="${xml(width)}"`
    if (height !== undefined) mediaAttributes += ` height="${xml(height)}"`
    if (frameRate !== undefined) {
      mediaAttributes += ` frameRate="${xml(frameRate)}"`
    }
  } else {
    const audioSampleRate = optionalPositiveInteger(
      track.audioSampleRate,
      'audio sampling rate',
    )
    if (audioSampleRate !== undefined) {
      mediaAttributes += ` audioSamplingRate="${xml(audioSampleRate)}"`
    }
  }

  return [
    `<AdaptationSet contentType="${kind}" mimeType="${xml(mimeType)}" segmentAlignment="true">`,
    `<Representation id="${kind}" bandwidth="${xml(bitrate)}" codecs="${xml(codecs)}"${mediaAttributes}>`,
    `<BaseURL>${xml(url)}</BaseURL>`,
    `<SegmentBase indexRange="${range(track.indexRange, `${kind} index range`)}">`,
    `<Initialization range="${range(track.initRange, `${kind} initialization range`)}"/>`,
    '</SegmentBase>',
    '</Representation>',
    '</AdaptationSet>',
  ].join('')
}

function isoDuration(durationMs: number): string {
  const wholeSeconds = Math.floor(durationMs / 1_000)
  const milliseconds = durationMs % 1_000
  if (milliseconds === 0) return `PT${wholeSeconds}S`

  const fraction = String(milliseconds).padStart(3, '0').replace(/0+$/, '')
  return `PT${wholeSeconds}.${fraction}S`
}

export function buildYouTubeMpd(playback: BrowserPlayback): string {
  if (
    playback === null ||
    typeof playback !== 'object' ||
    playback.mode !== 'dash' ||
    !playback.video ||
    !playback.audio
  ) {
    throw new Error('adaptive video and audio tracks are required')
  }

  const videoDuration = positiveInteger(
    playback.video.durationMs,
    'video duration',
  )
  const audioDuration = positiveInteger(
    playback.audio.durationMs,
    'audio duration',
  )
  const duration = isoDuration(Math.max(videoDuration, audioDuration))

  return [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" profiles="urn:mpeg:dash:profile:isoff-on-demand:2011" minBufferTime="PT1.5S"',
    ` mediaPresentationDuration="${duration}">`,
    '<Period start="PT0S">',
    representation(playback.video, 'video'),
    representation(playback.audio, 'audio'),
    '</Period>',
    '</MPD>',
  ].join('')
}
