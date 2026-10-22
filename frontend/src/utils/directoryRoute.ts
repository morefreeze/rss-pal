import type { RouteParameter } from '../api/sourceDirectory'

export interface DirectoryPathParameter { name: string; optional: boolean }
export function parseDirectoryRoute(path: string): { complex: boolean; parameters: DirectoryPathParameter[] } {
  const parameters: DirectoryPathParameter[] = []
  let complex = !path.startsWith('/')
  for (const segment of path.split('/').slice(1)) {
    const match = /^:([A-Za-z0-9_]+)(\?)?$/.exec(segment)
    if (match) parameters.push({ name: match[1], optional: !!match[2] })
    else if (/[:{}*()?]/.test(segment)) complex = true
  }
  return { complex, parameters }
}

function decoded(value: string): string {
  let result = value
  for (let index = 0; index < 4; index++) {
    const next = decodeURIComponent(result)
    if (next === result) return result
    result = next
  }
  return result
}
function validateNamespace(namespace: string) {
  if (namespace.split('/').some(segment => !/^[A-Za-z0-9_.-]+$/.test(segment) || segment === '.' || segment === '..')) throw new Error('无效的平台路径')
}
function validateEmbeddedURL(value: string) {
  if (/^[a-z][a-z0-9+.-]*:/i.test(value)) {
    if (!/^https?:\/\//i.test(value) || !publicDirectoryFeedURL(value.replace(/ /g, '%20'))) {
      throw new Error('嵌入网址必须为公开且无凭据的 HTTP(S) 地址')
    }
  }
}
function validatePath(path: string) {
  if (!path.startsWith('/') || path.startsWith('//') || /[\s\\\x00-\x1f\x7f?#:{}*]/.test(path)) {
    throw new Error('请填写具体的 RSSHub 路径，不含占位符、凭据、查询或跳转路径')
  }
  // Split the outer path before decoding: encoded URL slashes belong to a parameter.
  for (const segment of path.split('/')) {
    const plain = decoded(segment)
    if (/[\\\x00-\x1f\x7f{}*]/.test(plain) || plain.startsWith(':') || plain.split('/').some(part => part === '.' || part === '..')) {
      throw new Error('路径不能包含占位符或路径跳转')
    }
    validateEmbeddedURL(plain)
  }
}
export function manualDirectoryRouteURL(namespace: string, input: string): string {
  validateNamespace(namespace)
  const path = input.trim()
  validatePath(path)
  if (path !== `/${namespace}` && !path.startsWith(`/${namespace}/`)) throw new Error(`路径必须位于 /${namespace}/ 下`)
  return new URL(path, 'https://rsshub.app').href
}
export function buildDirectoryRouteURL(namespace: string, route: { path: string; parameters: Record<string, RouteParameter> }, values: Record<string, string>): string {
  validateNamespace(namespace)
  const parsed = parseDirectoryRoute(route.path)
  if (parsed.complex) throw new Error('此路由请填写完整路径')
  let gap = false
  const parts: string[] = []
  for (const part of route.path.split('/').slice(1)) {
    const match = /^:([A-Za-z0-9_]+)(\?)?$/.exec(part)
    if (!match) {
      const literal = decoded(part)
      if (literal === '.' || literal === '..' || /[\\\s\x00-\x1f\x7f?#:{}*]/.test(literal)) throw new Error('无效的路由路径')
      parts.push(part)
      continue
    }
    const name = match[1]
    const value = (values[name] ?? route.parameters[name]?.default ?? '').trim()
    if (!value) {
      if (!match[2]) throw new Error(`请填写 ${name}`)
      gap = true
      continue
    }
    if (gap) throw new Error('不能跳过中间的可选参数，请补齐前面的参数')
    const plain = decoded(value)
    if (plain.split(/[\\/]/).some(segment => segment === '.' || segment === '..') || /[\x00-\x1f\x7f]/.test(plain)) throw new Error('参数不能包含路径跳转')
    validateEmbeddedURL(plain)
    parts.push(encodeURIComponent(value))
  }
  // Parameter slashes are encoded and must remain inside their own segment.
  const path = `/${namespace}/${parts.join('/')}`
  if (/[{}*]/.test(path)) throw new Error('请填写具体路径')
  return new URL(path, 'https://rsshub.app').href
}

/** Best-effort browser-side validation; the preview API enforces DNS/IP SSRF checks. */
export function publicDirectoryFeedURL(input: string): string | null {
  try {
    if (input !== input.trim() || /[\\\x00-\x20\x7f]/.test(input)) return null
    if (input.startsWith('rsshub://')) {
      const path = `/${input.slice('rsshub://'.length)}`
      validatePath(path.split('?')[0])
      if (!/^\/[A-Za-z0-9_.-]+(?:\/|\?|$)/.test(path) || path.includes('#')) return null
      return publicDirectoryFeedURL(new URL(path, 'https://rsshub.app').href)
    }
    const authority = /^[A-Za-z]+:\/\/([^/?#]+)/.exec(input)?.[1]
    if (!authority || authority.includes(':') || authority.includes('@')) return null
    const url = new URL(input)
    const host = url.hostname.toLowerCase().replace(/\.$/, '')
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.port || url.hash) return null
    if (!host.includes('.') || /[:\[\]]/.test(host) || /^\d+(?:\.\d+){3}$/.test(host) || /(?:^|\.)(?:localhost|local|internal|lan|home|test|invalid)$/.test(host)) return null
    if (host === 'rsshub.app') validatePath(url.pathname)
    else for (const segment of url.pathname.split('/')) validateEmbeddedURL(decoded(segment))
    if ([...url.searchParams.keys()].some(key => /token|secret|cookie|password|passwd|credential|authorization|api.?key|access.?key/i.test(key))) return null
    return url.href
  } catch { return null }
}
