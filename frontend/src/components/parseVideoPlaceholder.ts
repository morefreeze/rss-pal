export type VideoPlatform = 'youtube' | 'bilibili'

export interface VideoEmbedData {
  platform: VideoPlatform
  id: string
  start?: number
  page?: number
}

const PLACEHOLDER_RE = /^\[\[video:(youtube|bilibili):([\w-]+)(?:\?([\w=&]+))?]]$/
const YT_ID_RE = /^[A-Za-z0-9_-]{11}$/
const BV_ID_RE = /^BV[0-9A-Za-z]{10}$/

export function parsePlaceholder(text: string): VideoEmbedData | null {
  const m = text.trim().match(PLACEHOLDER_RE)
  if (!m) return null
  const platform = m[1] as VideoPlatform
  const id = m[2]
  if (platform === 'youtube' && !YT_ID_RE.test(id)) return null
  if (platform === 'bilibili' && !BV_ID_RE.test(id)) return null
  const out: VideoEmbedData = { platform, id }
  if (m[3]) {
    const params = new URLSearchParams(m[3])
    const start = params.get('start')
    const page = params.get('page')
    if (start && /^\d+$/.test(start)) out.start = parseInt(start, 10)
    if (page && /^\d+$/.test(page)) out.page = parseInt(page, 10)
  }
  return out
}

export function parseStoredEmbedURL(rawURL: string, mediaType: string): VideoEmbedData | null {
  if (!rawURL || !mediaType) return null
  let u: URL
  try {
    u = new URL(rawURL)
  } catch {
    return null
  }
  if (mediaType === 'video/youtube') {
    if (!u.pathname.startsWith('/embed/')) return null
    const id = u.pathname.slice('/embed/'.length).split('/')[0]
    if (!YT_ID_RE.test(id)) return null
    const out: VideoEmbedData = { platform: 'youtube', id }
    const start = u.searchParams.get('start')
    if (start && /^\d+$/.test(start)) out.start = parseInt(start, 10)
    return out
  }
  if (mediaType === 'video/bilibili') {
    const id = u.searchParams.get('bvid') ?? ''
    if (!BV_ID_RE.test(id)) return null
    const out: VideoEmbedData = { platform: 'bilibili', id }
    const page = u.searchParams.get('page')
    const start = u.searchParams.get('t')
    if (page && /^\d+$/.test(page)) out.page = parseInt(page, 10)
    if (start && /^\d+$/.test(start)) out.start = parseInt(start, 10)
    return out
  }
  return null
}
