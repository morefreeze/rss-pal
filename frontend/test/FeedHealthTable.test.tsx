import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import FeedHealthTable from '../src/components/FeedHealthTable'
import type { FeedHealthRow } from '../src/api/client'

vi.mock('../src/api/client', () => ({ updateFeedStatus: vi.fn(), updateFeedWeight: vi.fn() }))

it('shows readable source identities and internal schemes as text', () => {
  const sources = [
    { feed_id: 45, feed_title: 'X 用户 · @alibaba_qwen', feed_url: 'extension://twitter:user/1/alibaba_qwen', feed_type: 'twitter:user' },
    { feed_id: 49, feed_title: 'X 书签', feed_url: 'extension://twitter:bookmarks/1/self', feed_type: 'twitter:bookmarks' },
    { feed_id: 27, feed_title: '⭐ 网摘', feed_url: 'bookmarklet://user/1', feed_type: 'clip' },
    { feed_id: 59, feed_title: 'Hacker Newsletter · 第 793 期', feed_url: 'https://buttondown.com/hacker-newsletter/archive/793/', feed_type: 'html' },
    { feed_id: 99, feed_title: '  ', feed_url: 'https://example.com/feed', feed_type: 'rss' },
  ]
  const rows = sources.map(s => ({ ...s, status: 'active', priority_weight: 1, produced: 0, ctr: null, read_completion: null, avg_duration_min: 0, last_active_at: null, value_score: null })) as FeedHealthRow[]
  render(<FeedHealthTable rows={rows} onChange={vi.fn()} />)
  for (const source of sources) expect(screen.getAllByText(source.feed_url).length).toBeGreaterThan(0)
  for (const label of ['X 用户', 'X 书签', '网摘', '网页', 'RSS']) expect(screen.getAllByText(label).length).toBeGreaterThan(0)
  expect(screen.getByText('X 用户 · @alibaba_qwen')).toBeTruthy()
  expect(document.querySelector('a[href^="extension:"], a[href^="bookmarklet:"]')).toBeNull()
})
