import { beforeEach, describe, expect, it } from 'vitest'
import {
  enrichDraftLinkTitle,
  loadDraftLinks,
  saveDraftLinks,
  type DraftLink,
} from '../src/utils/linkSetSelection'
import { normalizeHTTPURL } from '../src/utils/url'

describe('link set draft persistence', () => {
  beforeEach(() => localStorage.clear())

  it('round-trips ordered v2 drafts', () => {
    const links: DraftLink[] = [
      { url: 'https://example.com/a', title: 'A', addedAt: 100 },
      { url: 'https://example.com/b', title: 'B', addedAt: 200 },
    ]
    saveDraftLinks(7, links, localStorage, 1_000)
    expect(loadDraftLinks(7, localStorage, 2_000)).toEqual(links)
  })

  it('migrates v1 URL arrays into normalized ordered drafts', () => {
    localStorage.setItem('rsspal_batch_sel_7', JSON.stringify({
      urls: [
        'https://Example.com/a/?utm_source=x',
        'https://example.com/a',
        'mailto:a@example.com',
      ],
      savedAt: 1_000,
    }))
    expect(loadDraftLinks(7, localStorage, 2_000)).toEqual([{
      url: 'https://example.com/a',
      title: 'example.com',
      addedAt: 1_000,
    }])
  })

  it('expires drafts after 24 hours and removes the key', () => {
    saveDraftLinks(7, [{ url: 'https://example.com/a', title: 'A', addedAt: 1 }], localStorage, 1_000)
    expect(loadDraftLinks(7, localStorage, 1_000 + 24 * 60 * 60 * 1000 + 1)).toEqual([])
    expect(localStorage.getItem('rsspal_batch_sel_7')).toBeNull()
  })

  it('fails closed for corrupt data and storage errors', () => {
    localStorage.setItem('rsspal_batch_sel_7', '{bad json')
    expect(loadDraftLinks(7, localStorage, 2_000)).toEqual([])
    const broken = {
      getItem: () => { throw new Error('disabled') },
      setItem: () => { throw new Error('disabled') },
      removeItem: () => { throw new Error('disabled') },
      clear: () => {},
      key: () => null,
      length: 0,
    } satisfies Storage
    expect(loadDraftLinks(7, broken, 2_000)).toEqual([])
    expect(() => saveDraftLinks(7, [], broken, 2_000)).not.toThrow()
  })

  it('enriches only fallback titles', () => {
    const fallback = { url: 'https://example.com/a', title: 'example.com', addedAt: 1 }
    expect(enrichDraftLinkTitle(fallback, 'Readable title').title).toBe('Readable title')
    expect(enrichDraftLinkTitle({ ...fallback, title: 'Kept' }, 'Other').title).toBe('Kept')
  })
})

describe('normalizeHTTPURL', () => {
  it('resolves relative links and rejects non-http schemes', () => {
    expect(normalizeHTTPURL('/a/?utm_source=x#part', 'https://Example.com/post')).toBe('https://example.com/a')
    expect(normalizeHTTPURL('mailto:a@example.com', 'https://example.com/post')).toBeNull()
    expect(normalizeHTTPURL('javascript:void(0)', 'https://example.com/post')).toBeNull()
  })
})
