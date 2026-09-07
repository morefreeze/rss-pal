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
})
