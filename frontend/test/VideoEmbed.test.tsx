import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import VideoEmbed from '../src/components/VideoEmbed'

describe('VideoEmbed', () => {
  it('loads Bilibili embeds eagerly for WebKit compatibility', () => {
    render(<VideoEmbed platform="bilibili" id="BV1xL3y6cEVv" />)

    const iframe = screen.getByTitle('bilibili video BV1xL3y6cEVv')
    expect(iframe.getAttribute('loading')).toBeNull()
  })

  it('does not send the browser directly to a YouTube iframe', () => {
    render(<VideoEmbed platform="youtube" id="dQw4w9WgXcQ" />)

    expect(screen.queryByTitle('youtube video dQw4w9WgXcQ')).toBeNull()
    expect(screen.getByRole('link', { name: '在 YouTube 打开' }).getAttribute('href')).toBe(
      'https://www.youtube.com/watch?v=dQw4w9WgXcQ',
    )
  })
})
