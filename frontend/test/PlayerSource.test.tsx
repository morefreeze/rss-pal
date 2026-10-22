import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { PlayerProvider, usePlayer } from '../src/player/PlayerContext'
import type { ArticleListItem } from '../src/api/client'

vi.mock('../src/api/client', () => ({ getPlayback: vi.fn().mockResolvedValue({ position_seconds: 0, is_completed: false }), putPlayback: vi.fn().mockResolvedValue({}) }))

const child: ArticleListItem = { id: 3249, feed_id: 23, feed_title: '阮一峰的网络日志', title: 'DeskMind audio', url: 'https://deskmind.dev/launch', source: {key:'host:deskmind.dev', title:'deskmind.dev', url:'https://deskmind.dev/launch'}, media_url: 'https://deskmind.dev/audio.mp3', fetched_at:'2026-10-22T00:00:00Z', published_at:null, summary_brief:'', manual_tags:[] }

function PlayerProbe() {
 const player=usePlayer()
 return <><button onClick={() => player.playArticle(child)}>播放测试</button><span data-testid="player-source">{player.feedTitle}</span></>
}

describe('extracted audio attribution', () => {
 it('uses the publication source for the mini player and media metadata', async () => {
  vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {})
  render(<PlayerProvider><PlayerProbe /></PlayerProvider>)
  fireEvent.click(screen.getByRole('button', {name:'播放测试'}))
  expect(await screen.findByText('deskmind.dev')).toBeTruthy()
 })
})
