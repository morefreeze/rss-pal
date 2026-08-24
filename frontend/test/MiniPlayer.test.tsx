import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../src/player/PlayerContext', () => ({
  usePlayer: () => ({
    articleId: 1,
    title: 'Test episode',
    feedTitle: 'Test feed',
    loading: false,
    playing: true,
    position: 0,
    duration: 300,
    speed: 1,
    error: null,
    toggle: vi.fn(),
    skip: vi.fn(),
    seek: vi.fn(),
    setSpeed: vi.fn(),
    close: vi.fn(),
  }),
}))

vi.mock('../src/hooks/useBreakpoint', () => ({
  useBreakpoint: () => 'desktop',
}))

import MiniPlayer from '../src/components/MiniPlayer'

describe('MiniPlayer', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', class {
      observe() {}
      disconnect() {}
    })
  })

  it('removes the global input padding from the playback progress range', () => {
    render(<MiniPlayer />)

    expect(screen.getByRole('slider', { name: '播放进度' }).style.paddingInline).toBe('0px')
  })
})
