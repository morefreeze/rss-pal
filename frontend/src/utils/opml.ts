export const MAX_OPML_BYTES = 5 * 1024 * 1024
export interface OPMLEntry {
  url: string
  title: string
  feedType: 'rss' | 'html'
  error?: string
}

export function feedURLKey(raw: string): string {
  const url = new URL(raw.trim())
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) {
    throw new Error('订阅地址必须是无账号密码的 HTTP(S) 地址')
  }
  url.hash = ''
  return url.href
}

export function parseOPML(text: string): OPMLEntry[] {
  if (new TextEncoder().encode(text).length > MAX_OPML_BYTES) throw new Error('文件不能超过 5 MiB')
  if (/<!DOCTYPE|<!ENTITY/i.test(text)) throw new Error('不支持包含 DTD 或实体声明的 OPML 文件')
  const doc = new DOMParser().parseFromString(text, 'application/xml')
  if (doc.querySelector('parsererror') || doc.documentElement.localName !== 'opml') {
    throw new Error('OPML 文件格式错误，请检查 XML 是否完整')
  }
  const bodies = Array.from(doc.documentElement.children).filter(el => el.localName === 'body')
  if (bodies.length !== 1) throw new Error('OPML 文件必须包含一个 body 节点')
  const outlines = Array.from(bodies[0].querySelectorAll('outline[xmlUrl]'))
  if (!outlines.length) throw new Error('未找到有效的订阅条目')
  if (outlines.length > 5000) throw new Error('一次最多导入 5000 个订阅')
  return outlines.map(outline => {
    const entry: OPMLEntry = {
      url: (outline.getAttribute('xmlUrl') || '').trim(),
      title: (outline.getAttribute('title') || '').trim() || (outline.getAttribute('text') || '').trim(),
      feedType: outline.getAttribute('type')?.toLowerCase() === 'html' ? 'html' : 'rss',
    }
    try { entry.url = feedURLKey(entry.url) } catch { entry.error = '订阅地址必须是无账号密码的 HTTP(S) 地址' }
    return entry
  })
}
