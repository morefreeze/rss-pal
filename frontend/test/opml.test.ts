import { describe, it, expect } from 'vitest'
import { parseOPML, feedURLKey } from '../src/utils/opml'

describe('OPML parsing', () => {
  it('flattens folders and preserves titles, entities, types and invalid entry evidence', () => {
    const entries = parseOPML(`<?xml version="1.0"?><opml version="2.0"><head><title>Export</title></head><body><outline text="Folder"><outline title="中文 &amp; AI" text="fallback" xmlUrl="https://EXAMPLE.com/feed?a=1&amp;b=2"/><outline text="Web" type="html" xmlUrl="https://web.example/"/><outline xmlUrl="javascript:alert(1)"/></outline></body></opml>`)
    expect(entries).toHaveLength(3)
    expect(entries[0]).toMatchObject({ title: '中文 & AI', url: 'https://example.com/feed?a=1&b=2', feedType: 'rss' })
    expect(entries[1]).toMatchObject({ title: 'Web', feedType: 'html' })
    expect(entries[2].error).toContain('HTTP')
  })
  it.each(['<opml><body>', '<rss><body><outline xmlUrl="https://x.test"/></body></rss>', '<opml/>', '<!DOCTYPE opml [<!ENTITY x "feed">]><opml><body/></opml>'])('rejects invalid document before mutations: %s', text => {
    expect(() => parseOPML(text)).toThrow()
  })
  it('ignores head outlines and rejects an empty subscription file', () => {
    expect(() => parseOPML('<opml><head><outline xmlUrl="https://x.test"/></head><body/></opml>')).toThrow('订阅')
  })
  it('normalizes hosts, default ports and fragments but preserves query and trailing paths', () => {
    expect(feedURLKey('https://EXAMPLE.com:443/feed#x')).toBe(feedURLKey('https://example.com/feed'))
    expect(feedURLKey('https://example.com/feed/')).not.toBe(feedURLKey('https://example.com/feed'))
  })
  it('rejects excessive entries rather than silently truncating', () => {
    expect(() => parseOPML('<opml><body>' + '<outline xmlUrl="https://x.test"/>'.repeat(5001) + '</body></opml>')).toThrow('5000')
  })
})
it('submits the same canonical absolute URL that was validated', () => {
  const [entry] = parseOPML('<opml><body><outline xmlUrl="https:///example.com/feed#section"/></body></opml>')
  expect(entry.url).toBe('https://example.com/feed')
  expect(entry.error).toBeUndefined()
})
