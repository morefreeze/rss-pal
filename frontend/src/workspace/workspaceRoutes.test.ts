import { describe, expect, it } from 'vitest'
import { resolveWorkspaceRoute, safeListPath } from './workspaceRoutes'

describe('reading workspace routes', () => {
  it('keeps a validated briefing origin on article details', () => {
    expect(resolveWorkspaceRoute('/articles/42', '', { from: '/weekly?week=2026-11-09' })).toMatchObject({ module: 'briefing', listPath: '/weekly?week=2026-11-09', articleId: 42, cacheKey: 'weekly' })
  })
  it('recognizes clipping lists and details without changing their URLs', () => {
    expect(resolveWorkspaceRoute('/articles', '?view=clip')).toMatchObject({ module: 'clips', listPath: '/articles?view=clip', articleId: null })
    expect(resolveWorkspaceRoute('/articles/8', '', { from: '/clip' })).toMatchObject({ module: 'clips', listPath: '/clip', articleId: 8 })
  })
  it('rejects external and non-list origins', () => {
    for (const value of ['https://evil.test/articles', '//evil.test/articles', '/articles/8', '/settings', '/articles\\evil', '/articles#bad']) expect(safeListPath(value)).toBeNull()
    expect(resolveWorkspaceRoute('/articles/4', '', { from: '/settings' })).toMatchObject({ listPath: '/articles' })
  })
  it('keeps explore details inside explore and utility pages outside reading', () => {
    expect(resolveWorkspaceRoute('/explore/articles/7', '', { from: '/articles' })).toMatchObject({ module: 'explore', listPath: '/explore' })
    expect(resolveWorkspaceRoute('/settings', '')).toBeNull()
  })
})

describe('direct links', () => {
  it('does not apply unrelated stale session origins', () => {
    sessionStorage.setItem('articleEntryPath', '/weekly?week=2026-01-01')
    expect(resolveWorkspaceRoute('/articles/99', '')?.listPath).toBe('/articles')
  })
})
