import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import ReadingMeta from '../src/components/ReadingMeta'

describe('ReadingMeta video duration', () => {
  it('distinguishes video length from reading time', () => {
    render(<ReadingMeta wordCount={755} readingMinutes={3} mediaType="video/youtube" mediaDurationSeconds={97} />)
    expect(screen.getByText('视频 1:37')).toBeTruthy()
    expect(screen.getByText(/755 字 · 3 分钟/)).toBeTruthy()
  })
  it('shows hours even without text metrics', () => {
    render(<ReadingMeta mediaType="video/bilibili" mediaDurationSeconds={3723} />)
    expect(screen.getByText('视频 1:02:03')).toBeTruthy()
  })
  it.each([0, -1, NaN, Infinity, undefined])('does not invent unknown duration %s', seconds => {
    render(<ReadingMeta mediaType="video/youtube" mediaDurationSeconds={seconds} />)
    expect(screen.getByText('视频 · 时长未知')).toBeTruthy()
  })
  it('does not show video duration for ordinary articles', () => {
    const { container } = render(<ReadingMeta mediaDurationSeconds={97} />)
    expect(container.textContent).toBe('')
  })
})
