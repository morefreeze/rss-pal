import { describe, expect, it } from 'vitest'
import { buildDirectoryRouteURL, manualDirectoryRouteURL, parseDirectoryRoute, publicDirectoryFeedURL } from '../src/utils/directoryRoute'
const route = { path: '/user/:uid/:type?/:page?', parameters: { type: { description: '', default: 'posts' } } }
describe('directory route generation', () => {
  it('requires values, honors defaults and prevents optional gaps', () => {
    expect(() => buildDirectoryRouteURL('demo', route, {})).toThrow(/uid/)
    expect(buildDirectoryRouteURL('demo', route, { uid: '123' })).toBe('https://rsshub.app/demo/user/123/posts')
    expect(() => buildDirectoryRouteURL('demo', route, { uid: '123', type: '', page: '2' })).toThrow(/中间/)
  })
  it('encodes each parameter including unicode and slash', () => {
    expect(buildDirectoryRouteURL('demo', route, { uid: '中文/a' })).toBe('https://rsshub.app/demo/user/%E4%B8%AD%E6%96%87%2Fa/posts')
    expect(() => buildDirectoryRouteURL('demo', route, { uid: '..' })).toThrow()
  })
  it('supports dotted and nested upstream namespaces', () => {
    for (const namespace of ['picnob.info', '1x.com', 'gov/ah']) {
      expect(buildDirectoryRouteURL(namespace, { path: '/news', parameters: {} }, {})).toBe(`https://rsshub.app/${namespace}/news`)
      expect(manualDirectoryRouteURL(namespace, `/${namespace}/news`)).toBe(`https://rsshub.app/${namespace}/news`)
      expect(publicDirectoryFeedURL(`rsshub://${namespace}/news`)).toBe(`https://rsshub.app/${namespace}/news`)
    }
  })
  it('accepts public encoded URL parameters and rejects embedded private URLs', () => {
    for (const embedded of ['https://www.nasa.gov/feed/', 'https://example.com/feed/?s=hello world']) {
      const path = `/rss/${encodeURIComponent(embedded)}`
      expect(manualDirectoryRouteURL('rss', path)).toBe(`https://rsshub.app${path}`)
      expect(publicDirectoryFeedURL(`rsshub://${path.slice(1)}`)).toBe(`https://rsshub.app${path}`)
    }
    for (const embedded of ['http://localhost/feed', 'http://169.254.169.254/a', 'https://user:pass@example.com/feed', 'file:///etc/passwd', 'https://example.com/feed?token=secret']) {
      expect(() => manualDirectoryRouteURL('rss', `/rss/${encodeURIComponent(embedded)}`)).toThrow()
      expect(() => buildDirectoryRouteURL('rss', { path: '/:url', parameters: {} }, { url: embedded })).toThrow()
      expect(publicDirectoryFeedURL(`https://rsshub.app/rss/${encodeURIComponent(embedded)}`)).toBeNull()
    }
  })
  it('rejects unsafe static upstream route segments', () => {
    for (const path of ['/../other', '/%2e%2e/other', '/ok/../../outside', '/ok\\other']) {
      expect(() => buildDirectoryRouteURL('demo', { path, parameters: {} }, {})).toThrow()
    }
  })
  it('keeps regex and wildcard routes available through manual paths', () => {
    expect(parseDirectoryRoute('/mcr/product/*').complex).toBe(true)
    expect(parseDirectoryRoute('/81rc/:category{.+}?').complex).toBe(true)
    expect(manualDirectoryRouteURL('demo', '/demo/mcr/product/123')).toBe('https://rsshub.app/demo/mcr/product/123')
  })
  it.each(['/other/x', '//evil.test/demo/x', 'https://evil.test/demo/x', '/demo/../other', '/demo/%252e%252e/other', '/demo/:name', '/demo/product/*', '/demo/a?token=secret', '/demo/a#fragment', '/demo/a\\b'])('rejects unsafe manual path %s', input => {
    expect(() => manualDirectoryRouteURL('demo', input)).toThrow()
  })
})
describe('public directory examples', () => {
  it('converts RSSHub and accepts public feed URLs', () => {
    expect(publicDirectoryFeedURL('rsshub://bilibili/user/video/123')).toBe('https://rsshub.app/bilibili/user/video/123')
    expect(publicDirectoryFeedURL('https://example.com/feed.xml')).toBe('https://example.com/feed.xml')
    expect(publicDirectoryFeedURL('https://example.com/feed.xml?limit=20')).toBe('https://example.com/feed.xml?limit=20')
    expect(publicDirectoryFeedURL('rsshub://demo/user/123?limit=20')).toBe('https://rsshub.app/demo/user/123?limit=20')
  })
  it.each(['http://localhost/a', 'http://127.0.0.1/a', 'https://192.168.1.1/a', 'http://169.254.169.254/a', 'https://[::1]/a', 'https://foo.local/a', 'https://foo.internal/a', 'https://user:pass@example.com/a', 'https://example.com:8080/a', 'https://example.com:443/a', 'file:///etc/passwd', 'rsshub://http://localhost/a', 'rsshub://demo/a?token=secret', 'https://example.com/a?api_key=secret'])('rejects unsafe example %s', input => {
    expect(publicDirectoryFeedURL(input)).toBeNull()
  })
})
