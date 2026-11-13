import { describe, expect, it } from 'vitest'
import { playableMediaURL } from './mediaURL'

describe('playableMediaURL', () => {
  it('proxies external HTTP media so hotlink-protected audio can play', () => {
    expect(playableMediaURL('https://cdn.example.com/episode.mp3?x=1')).toBe(
      '/api/proxy/media?url=https%3A%2F%2Fcdn.example.com%2Fepisode.mp3%3Fx%3D1',
    )
  })

  it('keeps application media routes unchanged', () => {
    expect(playableMediaURL('/api/media/youtube/ticket/audio')).toBe(
      '/api/media/youtube/ticket/audio',
    )
  })

  it('supplies publisher context only for matching Lenny podcast enclosures', () => {
    const media = 'https://api.substack.com/feed/podcast/224221477/e714fff25978e0a4d943e3d0fd317f24.mp3'
    const publisher = 'https://www.lennysnewsletter.com/p/episode'
    const relay = `/api/proxy/media?url=${encodeURIComponent(media)}`
    expect(playableMediaURL(media, publisher + '?utm_source=feed#notes')).toBe(`${relay}&source=${encodeURIComponent(publisher)}`)
    expect(playableMediaURL(media, 'https://other.substack.com/p/episode')).toBe(relay)
    expect(playableMediaURL(media, 'https://www.lennysnewsletter.com.example.org/p/episode')).toBe(relay)
  })
})
