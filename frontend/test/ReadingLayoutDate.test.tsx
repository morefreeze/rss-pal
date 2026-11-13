import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import ReadingLayout from '../src/components/ReadingLayout'

function renderPublicationDate(published_at: string | null) {
  return render(<ReadingLayout
    article={{
      title: 'FAQ', url: 'https://lilianweng.github.io/faq/', published_at,
      word_count: 0, reading_minutes: 0, content: 'FAQ source text',
      summary_brief: '', summary_detailed: '',
    }}
    fontSize={18} fontFamily="sans" codeWrap={false}
    summaryOpen={true} onToggleSummary={vi.fn()}
    onExit={vi.fn()} onFontSize={vi.fn()} onFontFamily={vi.fn()} onCodeWrap={vi.fn()}
  />)
}

describe('ReadingLayout publication metadata', () => {
  it.each([null, '', '0001-01-01T00:00:00Z', '0001-01-01T00:00:00+08:00', 'invalid date'])('hides unavailable publication dates (%s) in reading mode', published_at => {
    const { container } = renderPublicationDate(published_at)
    expect(container.querySelector('.reading-meta')?.textContent).toBe(' · 原文链接')
    expect(container.querySelector('.reading-meta time')).toBeNull()
    expect(screen.getByRole('link', { name: '原文链接' }).getAttribute('href')).toBe('https://lilianweng.github.io/faq/')
    expect(screen.getByText('FAQ source text')).toBeTruthy()
  })

  it.each(['2026-07-27T00:00:00Z', '1970-01-01T00:00:00Z'])('keeps the valid publication date %s in reading mode', published_at => {
    renderPublicationDate(published_at)
    expect(screen.getByText(new Date(published_at).toLocaleString('zh-CN'))).toBeTruthy()
  })
})
