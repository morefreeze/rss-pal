import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SettingsPage from '../src/pages/SettingsPage'

vi.mock('../src/api/client', () => ({
  getShortcutConnections: vi.fn(async () => 0),
  getTemplates: vi.fn(async () => []),
  getAIConfig: vi.fn(async () => null),
  getBookmarkletToken: vi.fn(async () => 'test-token'),
}))

async function capture(html: string, hostname = 'example.test', mathJax?: (doc: Document) => unknown) {
  render(<MemoryRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}><SettingsPage user={{ is_admin: false }} /></MemoryRouter>)
  const link = await screen.findByRole('link', { name: '⭐ 发送到 RSS Pal' })
  const source = decodeURIComponent(link.getAttribute('href')!.slice('javascript:'.length))
  const doc = new DOMParser().parseFromString(html, 'text/html')
  const before = doc.documentElement.outerHTML
  const open = vi.fn((_url: string, _target: string) => ({}))
  const alert = vi.fn()
  // Execute the generated bookmarklet in an isolated DOM with its real receiver payload.
  new Function('document', 'location', 'window', 'alert', source)(doc, { hostname, href: `https://${hostname}/article` }, { open, MathJax: mathJax?.(doc) }, alert)
  expect(alert).not.toHaveBeenCalled()
  expect(doc.documentElement.outerHTML).toBe(before)
  const receiver = String(open.mock.calls[0]?.[0])
  expect(receiver.startsWith(`${window.location.origin}/bookmarklet-receiver.html#`)).toBe(true)
  expect(open.mock.calls[0]?.[1]).toBe('_blank')
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
    expect(install.getAttribute('href')).toBe('workflow://shortcuts/98ef2f6cfbe740e092908ab72ea9240e')
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

  it('removes only copy-code buttons inside pre while preserving literal code and other buttons', async () => {
    const samples = [
      'const message = "copy";\ncopy(value);\n',
      '<button class="copy-code">copy</button>\n',
      '@article{example,\n  title = "Code sample",\n  year = "2026"\n}\n',
    ]
    const blocks = samples.map((text, index) => {
      const code = document.createElement('code')
      code.textContent = text
      return `<pre id="sample-${index}">${code.outerHTML}<button class="copy-code">copy</button></pre>`
    }).join('')
    const doc = await capture(`<body><article>${blocks}<p><button id="body-copy" class="copy-code">正文复制按钮</button><button id="body-action">正文按钮</button></p><pre><button id="pre-action" class="run-code">run</button></pre></article></body>`)
    expect(doc.querySelectorAll('pre button.copy-code')).toHaveLength(0)
    samples.forEach((text, index) => {
      expect(doc.querySelector(`#sample-${index} code`)?.textContent).toBe(text)
      expect(doc.querySelector(`#sample-${index}`)?.textContent).toBe(text)
    })
    expect(doc.querySelector('#body-copy')?.textContent).toBe('正文复制按钮')
    expect(doc.querySelector('#body-action')?.textContent).toBe('正文按钮')
    expect(doc.querySelector('#pre-action')?.textContent).toBe('run')
  })

  it('preserves runtime-only MathJax TeX and display mode in the matching cloned containers', async () => {
    const inline = String.raw`\mathbf{x}_\text{adv}`
    const display = String.raw`\frac{x_1}{y} < z & \alpha > 0`
    const doc = await capture(`<body><article><p>完整正文 <mjx-container id="inline"><mjx-math>rendered inline</mjx-math></mjx-container></p>
      <mjx-container id="display"><mjx-math>rendered block</mjx-math></mjx-container>
      <img data-src="https://example.test/formula.jpg"></article></body>`, 'example.test', source => ({
      startup: { document: { math: new Set([
        { typesetRoot: source.querySelector('#display'), math: display, display: true },
        { typesetRoot: source.querySelector('#inline'), math: inline, display: false },
      ]) } },
    }))
    expect(doc.querySelector('#inline annotation[encoding="application/x-tex"]')?.textContent).toBe(inline)
    expect(doc.querySelector('#display annotation[encoding="application/x-tex"]')?.textContent).toBe(display)
    expect(doc.querySelector('#inline')?.getAttribute('display')).toBe('false')
    expect(doc.querySelector('#display')?.getAttribute('display')).toBe('true')
    expect(doc.querySelector('#inline mjx-math')?.textContent).toBe('rendered inline')
    expect(doc.querySelector('img')?.getAttribute('src')).toBe('https://example.test/formula.jpg')
  })

  it('keeps existing TeX annotations and inert MathJax v2 scripts alongside JSON-LD', async () => {
    const metadata = { '@type': 'Article', datePublished: '2026-09-20' }
    const doc = await capture(String.raw`<head><script type="application/ld+json">${JSON.stringify(metadata)}</script>
      <script>window.unwanted=true</script><script type="module" src="/app.js"></script><style>math{color:red}</style></head>
      <body><article><p>完整正文
      <span class="katex"><math><semantics><annotation encoding="application/x-tex">\sqrt{x+7} \ge 0</annotation></semantics></math></span>
      <math id="mathml"><semantics><annotation encoding="application/x-tex">a^2+b^2=c^2</annotation></semantics></math>
      <mjx-container id="annotated" display="true"><annotation encoding="application/x-tex">\frac{a}{b}</annotation></mjx-container>
      <script type="math/tex">x \ge 1</script><script type="math/tex; mode=display">y=\frac{a}{b}</script></p></article></body>`, 'example.test', source => ({
      startup: { document: { math: [{ typesetRoot: source.querySelector('#annotated'), math: 'do-not-replace-existing-TeX', display: true }] } },
    }))
    expect(doc.querySelector('.katex annotation')?.textContent).toBe(String.raw`\sqrt{x+7} \ge 0`)
    expect(doc.querySelector('#mathml annotation')?.textContent).toBe('a^2+b^2=c^2')
    expect(doc.querySelectorAll('#annotated annotation')).toHaveLength(1)
    expect(doc.querySelector('#annotated annotation')?.textContent).toBe(String.raw`\frac{a}{b}`)
    expect(Array.from(doc.querySelectorAll('script')).map(script => script.getAttribute('type'))).toEqual([
      'application/ld+json', 'math/tex', 'math/tex; mode=display',
    ])
    expect(doc.querySelector('script[type="math/tex"]')?.textContent).toBe(String.raw`x \ge 1`)
    expect(doc.querySelector('script[type="math/tex; mode=display"]')?.textContent).toBe(String.raw`y=\frac{a}{b}`)
    expect(JSON.parse(doc.querySelector('script[type="application/ld+json"]')!.textContent!)).toEqual(metadata)
    expect(doc.querySelector('style')).toBeNull()
  })

  it('skips malformed or unmatched MathJax items without losing later TeX or other content', async () => {
    const doc = await capture('<body><article><p>完整正文</p><mjx-container id="broken">rendered broken</mjx-container><mjx-container id="nontex">rendered MathML</mjx-container><mjx-container id="empty">rendered empty</mjx-container><mjx-container id="valid">rendered valid</mjx-container><img data-src="https://example.test/full.jpg"></article></body>', 'example.test', source => ({
      startup: { document: { math: new Set([
        { typesetRoot: source.querySelector('#broken'), get math() { throw new Error('bad math item') } },
        { typesetRoot: source.createElement('mjx-container'), math: 'unmatched', display: false },
        { typesetRoot: source.querySelector('#nontex'), math: '<math><mi>x</mi></math>', inputJax: { name: 'MathML' }, display: false },
        { typesetRoot: source.querySelector('#empty'), math: '  ', display: false },
        { typesetRoot: source.querySelector('#valid'), math: String.raw`\alpha + \beta`, display: false },
      ]) } },
    }))
    expect(doc.querySelectorAll('annotation')).toHaveLength(1)
    expect(doc.querySelector('#valid annotation')?.textContent).toBe(String.raw`\alpha + \beta`)
    expect(doc.querySelector('article p')?.textContent).toBe('完整正文')
    expect(doc.querySelector('img')?.getAttribute('src')).toBe('https://example.test/full.jpg')
  })

  const unavailableMathRuntimes: [string, ((doc: Document) => unknown) | undefined][] = [
    ['missing', undefined],
    ['throwing startup', () => ({ get startup() { throw new Error('unavailable runtime') } })],
    ['throwing iterator', () => ({ startup: { document: { math: { [Symbol.iterator]() { throw new Error('unavailable math list') } } } } })],
  ]
  it.each(unavailableMathRuntimes)('captures the article when the MathJax runtime is %s without guessing TeX', async (_name, runtime) => {
    const doc = await capture('<body><article><p>完整正文</p><mjx-container><mjx-math>rendered x²</mjx-math></mjx-container><img data-src="https://example.test/full.jpg"></article></body>', 'example.test', runtime)
    expect(doc.querySelector('article p')?.textContent).toBe('完整正文')
    expect(doc.querySelector('img')?.getAttribute('src')).toBe('https://example.test/full.jpg')
    expect(doc.querySelector('mjx-math')?.textContent).toBe('rendered x²')
    expect(doc.querySelector('annotation')).toBeNull()
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
