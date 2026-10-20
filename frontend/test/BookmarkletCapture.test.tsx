import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SettingsPage from '../src/pages/SettingsPage'

vi.mock('../src/api/client', () => ({
  getTemplates: vi.fn(async () => []),
  getAIConfig: vi.fn(async () => null),
  getBookmarkletToken: vi.fn(async () => 'test-token'),
}))

async function capture(html: string, hostname = 'example.test') {
  render(<MemoryRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}><SettingsPage user={{ is_admin: false }} /></MemoryRouter>)
  const link = await screen.findByRole('link', { name: '⭐ 发送到 RSS Pal' })
  const source = decodeURIComponent(link.getAttribute('href')!.slice('javascript:'.length))
  const doc = new DOMParser().parseFromString(html, 'text/html')
  const before = doc.documentElement.outerHTML
  const open = vi.fn((_url: string, _target: string) => ({}))
  const alert = vi.fn()
  // Execute the generated bookmarklet in an isolated DOM with its real receiver payload.
  new Function('document', 'location', 'window', 'alert', source)(doc, { hostname, href: `https://${hostname}/article` }, { open }, alert)
  expect(alert).not.toHaveBeenCalled()
  expect(doc.documentElement.outerHTML).toBe(before)
  const receiver = String(open.mock.calls[0]?.[0])
  const payload = JSON.parse(decodeURIComponent(escape(atob(receiver.split('#')[1]))))
  expect(payload.token).toBe('test-token')
  expect(payload.url).toBe(`https://${hostname}/article`)
  return new DOMParser().parseFromString(payload.html, 'text/html')
}

beforeEach(() => { window.location.hash = '#tools' })

describe('Settings bookmarklet capture', () => {
  it('offers the published Apple shortcut installer instead of manual configuration', async () => {
    render(<MemoryRouter><SettingsPage user={{ is_admin: false }} /></MemoryRouter>)
    const install = await screen.findByRole('link', { name: '添加到快捷指令' })
    expect(install.getAttribute('href')).toBe('https://www.icloud.com/shortcuts/bee8c10b751e47a18d87fb75f889b3d3')
    expect(screen.queryByRole('link', { name: '配置“加入 RSS Pal”快捷指令' })).toBeNull()
  })

  it('preserves publication JSON-LD, full article, lazy images and math without executable scripts', async () => {
    const metadata = { '@type': 'Article', datePublished: '2026-09-20T08:30:00+08:00', dateModified: '2026-10-14' }
    const doc = await capture(`<head><script type="application/ld+json">${JSON.stringify(metadata)}</script>
      <script>window.unwanted=true</script><script type="module" src="/app.js"></script><style>body{color:red}</style></head>
      <body><article><p>完整正文</p><img data-src="https://example.test/full.jpg" data-srcset="https://example.test/2x.jpg 2x"><math><mi>x</mi></math></article></body>`)
    expect(doc.querySelectorAll('script')).toHaveLength(1)
    expect(JSON.parse(doc.querySelector('script[type="application/ld+json"]')!.textContent!)).toEqual(metadata)
    expect(doc.querySelector('article p')?.textContent).toBe('完整正文')
    expect(doc.querySelector('img')?.getAttribute('src')).toBe('https://example.test/full.jpg')
    expect(doc.querySelector('img')?.getAttribute('srcset')).toBe('https://example.test/2x.jpg 2x')
    expect(doc.querySelector('math')?.textContent).toBe('x')
    expect(doc.querySelector('style')).toBeNull()
  })

  it('adds explicit WeChat publication time to metadata before cleaning', async () => {
    const doc = await capture('<head><title>文章</title></head><body><em id="publish_time">2026年9月20日 08:30</em><div id="js_content"><p>完整正文</p></div></body>', 'mp.weixin.qq.com')
    expect(doc.querySelector('meta[name="rsspal:wechat_publish_time"]')?.getAttribute('content')).toBe('2026年9月20日 08:30')
    expect(doc.querySelector('meta[property="article:published_time"]')).toBeNull()
    expect(doc.querySelector('#js_content p')?.textContent).toBe('完整正文')
  })

  it('keeps Article JSON-LD above a different visible WeChat publication date', async () => {
    const metadata = { '@type': 'Article', datePublished: '2026-09-20T08:30:00+08:00' }
    const doc = await capture(`<head><script type="application/ld+json">${JSON.stringify(metadata)}</script></head><body><em id="publish_time">2026年9月19日 08:30</em></body>`, 'mp.weixin.qq.com')
    expect(JSON.parse(doc.querySelector('script[type="application/ld+json"]')!.textContent!)).toEqual(metadata)
    expect(doc.querySelector('meta[name="rsspal:wechat_publish_time"]')?.getAttribute('content')).toBe('2026年9月19日 08:30')
    expect(doc.querySelector('meta[property="article:published_time"]')).toBeNull()
  })

  it('does not use relative display, modification or comment time as publication time', async () => {
    const doc = await capture('<head><meta property="article:modified_time" content="2026-10-14T10:00:00+08:00"></head><body><em id="publish_time">昨天</em><div class="comment_area"><time datetime="2026-10-14T11:00:00+08:00">评论</time></div></body>', 'mp.weixin.qq.com')
    expect(doc.querySelector('meta[property="article:published_time"]')).toBeNull()
  })

  it.each(['name="datePublished"', 'name="publish_date"', 'name="publication_date"'])(
    'preserves explicit head meta %s without promoting a conflicting visible date', async attributes => {
      const doc = await capture(`<head><meta ${attributes} content="Sun, 20 Sep 2026 08:30:00 +0800"></head><body><em id="publish_time">2026年9月19日 08:30</em></body>`, 'mp.weixin.qq.com')
      expect(doc.querySelector(`head meta[${attributes}]`)?.getAttribute('content')).toBe('Sun, 20 Sep 2026 08:30:00 +0800')
      expect(doc.querySelector('head meta[property="article:published_time"]')).toBeNull()
      expect(doc.querySelector('head meta[name="rsspal:wechat_publish_time"]')?.getAttribute('content')).toBe('2026年9月19日 08:30')
    },
  )

  it.each(['class="comment_area"', 'id="discussion-comments"', 'class="related-articles"', 'id="related-articles"', 'id="recommend-list"', 'itemscope itemtype="https://schema.org/Comment"'])('does not promote metadata or publish_time from %s into the head', async area => {
    const doc = await capture(`<head></head><body><div ${area}><meta itemprop="datePublished" content="2026-10-14T10:00:00+08:00"><em id="publish_time">2026年10月14日 10:00</em></div></body>`, 'mp.weixin.qq.com')
    expect(doc.querySelector('head meta[property="article:published_time"]')).toBeNull()
    expect(doc.querySelector('head meta[name="rsspal:wechat_publish_time"]')).toBeNull()
  })
})
