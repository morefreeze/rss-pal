import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import VideoEmbed from '../src/components/VideoEmbed'

describe('VideoEmbed', () => {
  it('loads Bilibili embeds eagerly for WebKit compatibility', () => {
    render(<VideoEmbed platform="bilibili" id="BV1xL3y6cEVv" />)

    const iframe = screen.getByTitle('bilibili video BV1xL3y6cEVv')
    expect(iframe.getAttribute('loading')).toBeNull()
  })

  it('keeps lazy loading for YouTube embeds', () => {
    render(<VideoEmbed platform="youtube" id="dQw4w9WgXcQ" />)

    const iframe = screen.getByTitle('youtube video dQw4w9WgXcQ')
    expect(iframe.getAttribute('loading')).toBe('lazy')
  })
})
