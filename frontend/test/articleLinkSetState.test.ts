import { describe, expect, it } from 'vitest'
import {
  addDraftTargets,
  buildFetchedURLSet,
  enrichDraftLinks,
  removeDraftURLs,
  type DraftLink,
} from '../src/utils/linkSetSelection'

const a: DraftLink = { url: 'https://a.example/', title: 'A saved', addedAt: 1 }
const b: DraftLink = { url: 'https://b.example/', title: 'b.example', addedAt: 2 }

describe('article link draft transitions', () => {
  it('builds fetched state from the server URL projection, including hidden children', () => {
    const result = buildFetchedURLSet(
      ['https://visible.example/path/', 'https://hidden.example/?utm_source=rss'],
      'https://parent.example/article',
    )
    expect([...result]).toEqual([
      'https://visible.example/path',
      'https://hidden.example/',
    ])
  })

  it('adds normalized targets in order while ignoring fetched and duplicate URLs', () => {
    const result = addDraftTargets(
      [a],
      [
        { url: 'https://A.example/?utm_source=x', title: 'Duplicate A' },
        { url: 'https://b.example/', title: 'B title' },
        { url: 'https://b.example', title: 'Duplicate B' },
        { url: 'https://c.example/', title: 'Fetched C' },
        { url: 'mailto:test@example.com', title: 'Mail' },
      ],
      new Set(['https://c.example/']),
      100,
    )
    expect(result).toEqual([
      a,
      { url: 'https://b.example/', title: 'B title', addedAt: 100 },
    ])
    expect(result[0]).toBe(a)
  })

  it('removes only submitted URLs and preserves remaining object identity', () => {
    const result = removeDraftURLs([a, b], new Set([a.url]))
    expect(result).toEqual([b])
    expect(result[0]).toBe(b)
  })

  it('enriches fallback titles but keeps explicit titles', () => {
    const result = enrichDraftLinks([a, b], [
      { url: a.url, title: 'Replacement A' },
      { url: b.url, title: 'Readable B title' },
    ])
    expect(result).toEqual([a, { ...b, title: 'Readable B title' }])
    expect(result[0]).toBe(a)
  })

  it('preserves array identity for no-op transitions', () => {
    const existing = [a]
    expect(addDraftTargets(existing, [{ url: a.url, title: 'Duplicate' }], new Set(), 10)).toBe(existing)
    expect(removeDraftURLs(existing, new Set(['https://missing.example/']))).toBe(existing)
    expect(enrichDraftLinks(existing, [{ url: a.url, title: 'Other title' }])).toBe(existing)
  })
})
