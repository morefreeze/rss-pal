import { describe, expect, it } from 'vitest'
import type { WeeklyDigest } from '../api/client'
import { weeklyEmptyStateMessage } from './weeklyGenerationStatus'

const digest = (overrides: Partial<WeeklyDigest>): WeeklyDigest => ({
  week_start: '2026-08-24',
  intro_text: '',
  articles: [],
  ...overrides,
})

describe('weeklyEmptyStateMessage', () => {
  it('shows the scheduled Beijing hour', () => {
    expect(weeklyEmptyStateMessage(digest({
      pending: true,
      generation_status: 'scheduled',
      estimated_generation_at: '2026-08-31T05:00:00+08:00',
    }))).toBe('预计于 2026-08-31 05:00（北京时间）开始生成')
  })

  it('shows pending after generation becomes eligible', () => {
    expect(weeklyEmptyStateMessage(digest({ pending: true, generation_status: 'pending' })))
      .toBe('周报生成中，稍后刷新…')
  })

  it('shows that an expired digest will not be generated', () => {
    expect(weeklyEmptyStateMessage(digest({ pending: false, generation_status: 'not_planned' })))
      .toBe('该周报已过自动生成范围，不再生成。')
  })

  it('keeps the legacy pending fallback', () => {
    expect(weeklyEmptyStateMessage(digest({ pending: true })))
      .toBe('周报生成中，稍后刷新…')
  })

  it('falls back when a scheduled timestamp is invalid', () => {
    expect(weeklyEmptyStateMessage(digest({
      pending: true,
      generation_status: 'scheduled',
      estimated_generation_at: 'invalid',
    }))).toBe('周报生成中，稍后刷新…')
  })

  it('returns null for a ready empty digest', () => {
    expect(weeklyEmptyStateMessage(digest({ pending: false, generation_status: 'ready' }))).toBeNull()
  })
})
