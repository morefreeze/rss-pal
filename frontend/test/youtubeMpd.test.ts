import { describe, expect, it } from 'vitest'

import type { BrowserPlayback } from '../src/youtube/bridge'
import { buildYouTubeMpd } from '../src/youtube/mpd'

function adaptiveFixture(): BrowserPlayback {
  return {
    mode: 'dash',
    quality: 1080,
    expiresAt: '2030-01-01T00:00:00.000Z',
    video: {
      url: 'https://rr1---sn-a5mekn6z.googlevideo.com/videoplayback?itag=137&sig=video-signature&n=video-token',
      mimeType: 'video/mp4',
      codecs: 'avc1.640028',
      bitrate: 3_500_000,
      initRange: { start: 0, end: 739 },
      indexRange: { start: 740, end: 1_251 },
      durationMs: 120_000,
      width: 1920,
      height: 1080,
      frameRate: 30,
    },
    audio: {
      url: 'https://rr1---sn-a5mekn6z.googlevideo.com/videoplayback?itag=140&sig=audio-signature&n=audio-token',
      mimeType: 'audio/mp4',
      codecs: 'mp4a.40.2',
      bitrate: 128_000,
      initRange: { start: 0, end: 721 },
      indexRange: { start: 722, end: 1_201 },
      durationMs: 120_000,
      audioSampleRate: 48_000,
    },
  }
}

function parse(xml: string): XMLDocument {
  return new DOMParser().parseFromString(xml, 'application/xml')
}

describe('buildYouTubeMpd', () => {
  it('builds a static ISO-on-demand manifest with separate adaptive tracks', () => {
    const playback = adaptiveFixture()
    const xml = buildYouTubeMpd(playback)
    const document = parse(xml)

    expect(document.querySelector('parsererror')).toBeNull()
    expect(document.documentElement.tagName).toBe('MPD')
    expect(document.documentElement.getAttribute('type')).toBe('static')
    expect(document.documentElement.getAttribute('profiles')).toBe(
      'urn:mpeg:dash:profile:isoff-on-demand:2011',
    )
    expect(document.documentElement.getAttribute('mediaPresentationDuration'))
      .toBe('PT120S')
    expect(document.querySelector('Period')?.getAttribute('start')).toBe('PT0S')

    const video = document.querySelector(
      'AdaptationSet[contentType="video"] Representation',
    )
    expect(video?.getAttribute('bandwidth')).toBe('3500000')
    expect(video?.getAttribute('codecs')).toBe('avc1.640028')
    expect(video?.getAttribute('width')).toBe('1920')
    expect(video?.getAttribute('height')).toBe('1080')
    expect(video?.getAttribute('frameRate')).toBe('30')
    expect(
      document.querySelector('AdaptationSet[contentType="video"] BaseURL')
        ?.textContent,
    ).toBe(playback.video?.url)
    expect(
      document
        .querySelector('AdaptationSet[contentType="video"] SegmentBase')
        ?.getAttribute('indexRange'),
    ).toBe('740-1251')
    expect(
      document
        .querySelector('AdaptationSet[contentType="video"] Initialization')
        ?.getAttribute('range'),
    ).toBe('0-739')

    const audio = document.querySelector(
      'AdaptationSet[contentType="audio"] Representation',
    )
    expect(audio?.getAttribute('bandwidth')).toBe('128000')
    expect(audio?.getAttribute('codecs')).toBe('mp4a.40.2')
    expect(audio?.getAttribute('audioSamplingRate')).toBe('48000')
    expect(
      document.querySelector('AdaptationSet[contentType="audio"] BaseURL')
        ?.textContent,
    ).toBe(playback.audio?.url)
    expect(
      document
        .querySelector('AdaptationSet[contentType="audio"] SegmentBase')
        ?.getAttribute('indexRange'),
    ).toBe('722-1201')
    expect(
      document
        .querySelector('AdaptationSet[contentType="audio"] Initialization')
        ?.getAttribute('range'),
    ).toBe('0-721')
  })

  it('escapes signed URLs and XML metacharacters without adding markup', () => {
    const playback = adaptiveFixture()
    const video = playback.video!
    video.url =
      'https://r1.googlevideo.com/videoplayback?sig=a&note=<tag>"quoted"\'apostrophe\''
    video.mimeType = 'video/mp4"><Injected attr=\'bad\'>&'
    video.codecs = 'avc1.640028"><Injected>&\''

    const xml = buildYouTubeMpd(playback)
    const document = parse(xml)

    expect(document.querySelector('parsererror')).toBeNull()
    expect(document.querySelector('Injected')).toBeNull()
    expect(
      document.querySelector('AdaptationSet[contentType="video"] BaseURL')
        ?.textContent,
    ).toBe(video.url)
    expect(
      document
        .querySelector('AdaptationSet[contentType="video"]')
        ?.getAttribute('mimeType'),
    ).toBe(video.mimeType)
    expect(
      document
        .querySelector('AdaptationSet[contentType="video"] Representation')
        ?.getAttribute('codecs'),
    ).toBe(video.codecs)
    expect(xml).toContain('&amp;')
    expect(xml).toContain('&lt;')
    expect(xml).toContain('&gt;')
    expect(xml).toContain('&quot;')
    expect(xml).toContain('&apos;')
  })

  it('rejects progressive playback and missing adaptive tracks', () => {
    const progressive: BrowserPlayback = {
      mode: 'progressive',
      quality: 720,
      expiresAt: '2030-01-01T00:00:00.000Z',
      progressive: {
        url: 'https://r1.googlevideo.com/videoplayback?itag=22&sig=test',
        mimeType: 'video/mp4',
        height: 720,
      },
    }
    const missingAudio = adaptiveFixture()
    delete missingAudio.audio

    expect(() => buildYouTubeMpd(progressive)).toThrow(
      'adaptive video and audio tracks are required',
    )
    expect(() => buildYouTubeMpd(missingAudio)).toThrow(
      'adaptive video and audio tracks are required',
    )
  })

  it.each([
    ['zero duration', { video: { durationMs: 0 } }],
    ['infinite duration', { audio: { durationMs: Number.POSITIVE_INFINITY } }],
    ['NaN bitrate', { video: { bitrate: Number.NaN } }],
    ['infinite frame rate', { video: { frameRate: Number.POSITIVE_INFINITY } }],
    ['negative sampling rate', { audio: { audioSampleRate: -1 } }],
    ['fractional range', { video: { indexRange: { start: 740.5, end: 1251 } } }],
    ['backwards range', { audio: { initRange: { start: 721, end: 0 } } }],
  ])('rejects unsafe runtime metadata: %s', (_name, patch) => {
    const playback = adaptiveFixture() as BrowserPlayback & Record<string, any>

    for (const [trackName, values] of Object.entries(patch)) {
      Object.assign(playback[trackName], values)
    }

    expect(() => buildYouTubeMpd(playback)).toThrow()
  })

  it('returns the same in-memory string for the same playback contract', () => {
    const playback = adaptiveFixture()

    expect(buildYouTubeMpd(playback)).toBe(buildYouTubeMpd(playback))
  })
})
