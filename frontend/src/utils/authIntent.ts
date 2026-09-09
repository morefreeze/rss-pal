export type AuthIntent =
  | { kind: 'use'; returnTo: '/articles' }
  | { kind: 'subscribe'; returnTo: '/feeds'; source: string }

const USE_INTENT: AuthIntent = { kind: 'use', returnTo: '/articles' }
const MAX_URL_LENGTH = 2048

function isIPLiteral(hostname: string): boolean {
  const host = hostname.replace(/^\[|\]$/g, '')
  return /^\d+\.\d+\.\d+\.\d+$/.test(host) || host.includes(':')
}

function isNonPublicHostname(hostname: string): boolean {
  const host = hostname.toLowerCase().replace(/^\[|\]$/g, '').replace(/\.+$/, '')
  if (host === 'localhost' || host.endsWith('.localhost')) return true

  if (/^\d+\.\d+\.\d+\.\d+$/.test(host)) {
    const octets = host.split('.').map(Number)
    if (octets.some(octet => octet < 0 || octet > 255)) return true
    const [a, b, c] = octets
    return a === 0
      || a === 10
      || a === 127
      || (a === 100 && b >= 64 && b <= 127)
      || (a === 169 && b === 254)
      || (a === 172 && b >= 16 && b <= 31)
      || (a === 192 && b === 0)
      || (a === 192 && b === 88 && c === 99)
      || (a === 192 && b === 168)
      || (a === 198 && (b === 18 || b === 19))
      || (a === 198 && b === 51 && c === 100)
      || (a === 203 && b === 0 && c === 113)
      || a >= 224
  }

  if (host.includes(':')) {
    if (host === '::' || host === '::1' || host.startsWith('::ffff:')) return true
    const first = Number.parseInt(host.split(':', 1)[0] || '0', 16)
    return (first & 0xfe00) === 0xfc00
      || (first & 0xffc0) === 0xfe80
      || (first & 0xff00) === 0xff00
      || host.startsWith('2001:db8:')
  }

  return false
}

/**
 * Returns the trimmed source only when it is safe to fetch as a public URL.
 * Keeping the caller's URL spelling (rather than URL.href) also makes encoding
 * predictable: authSearch is the only layer which percent-encodes it.
 */
export function safePublicSourceURL(rawURL?: string): string | null {
  const value = rawURL?.trim()
  if (!value || value.length > MAX_URL_LENGTH || value.startsWith('//')) return null

  let parsed: URL
  try {
    parsed = new URL(value)
  } catch {
    return null
  }

  if (!['http:', 'https:'].includes(parsed.protocol)) return null
  if (parsed.username || parsed.password) return null
  if (isNonPublicHostname(parsed.hostname) || isIPLiteral(parsed.hostname)) return null
  return value
}

function subscribePostAuthURL(source: string): string {
  return `/feeds?add=1&source=${encodeURIComponent(source)}`
}

export function parseAuthIntent(search: string): AuthIntent {
  // URLSearchParams deliberately tolerates broken percent escapes. Authentication
  // intent parsing must fail closed instead, so malformed input cannot be
  // normalized into a different fetch target.
  try {
    decodeURIComponent(search.replace(/\+/g, ' '))
  } catch {
    return USE_INTENT
  }

  const params = new URLSearchParams(search.startsWith('?') ? search.slice(1) : search)
  if (params.getAll('intent').length !== 1 || params.get('intent') !== 'subscribe') {
    return USE_INTENT
  }
  const sources = params.getAll('source')
  if (sources.length !== 1) return USE_INTENT

  const source = safePublicSourceURL(sources[0])
  if (!source || subscribePostAuthURL(source).length > MAX_URL_LENGTH) return USE_INTENT
  return { kind: 'subscribe', returnTo: '/feeds', source }
}

export function authSearch(intent: AuthIntent): string {
  if (intent.kind === 'use') return '?intent=use'
  return `?intent=subscribe&source=${encodeURIComponent(intent.source)}`
}

export function postAuthURL(intent: AuthIntent): string {
  return intent.kind === 'subscribe' ? subscribePostAuthURL(intent.source) : '/articles'
}
