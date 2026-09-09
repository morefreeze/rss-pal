import MarkdownArticle from './MarkdownArticle'
import ReadingMeta from './ReadingMeta'
import SummaryMarkdown from './SummaryMarkdown'
import VideoEmbed from './VideoEmbed'
import { parseStoredEmbedURL } from './parseVideoPlaceholder'
import { authSearch, parseAuthIntent, safePublicSourceURL } from '../utils/authIntent'

export { safePublicSourceURL } from '../utils/authIntent'

export interface SharedArticleSnapshot {
  title: string
  url: string
  feed_title?: string
  published_at?: string | null
  word_count?: number
  reading_minutes?: number
  summary_brief?: string
  summary_detailed?: string
  content: string
  media_url?: string
  media_type?: string
  media_duration_seconds?: number
  image_dimensions?: Record<string, [number, number]>
  snapshotted_at: string
}

type Props = { article: SharedArticleSnapshot }

function formatDate(value?: string | null): string {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleString('zh-CN')
}

function formatDuration(seconds?: number): string {
  if (!seconds || seconds <= 0 || !Number.isFinite(seconds)) return '时长未知'
  const minutes = Math.floor(seconds / 60)
  const remainder = Math.floor(seconds % 60)
  return `${minutes}分${remainder.toString().padStart(2, '0')}秒`
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

function parseSafePublicURL(rawURL?: string): { value: string; parsed: URL } | null {
  const value = rawURL?.trim()
  if (!value || value.startsWith('//')) return null
  let parsed: URL
  try {
    parsed = new URL(value)
  } catch {
    return null
  }
  if (!['http:', 'https:'].includes(parsed.protocol)) return null
  if (parsed.username || parsed.password) return null
  if (isNonPublicHostname(parsed.hostname)) return null
  return { value, parsed }
}

function normalizedDecodedPathname(pathname: string): string | null {
  let decoded = pathname
  for (let pass = 0; pass < 4; pass += 1) {
    let next: string
    try {
      next = decodeURIComponent(decoded)
    } catch {
      return null
    }
    if (next === decoded) break
    decoded = next
  }
  if (/%[0-9a-f]{2}/i.test(decoded)) return null

  const segments: string[] = []
  for (const segment of decoded.replace(/\\/g, '/').split('/')) {
    if (!segment || segment === '.') continue
    if (segment === '..') segments.pop()
    else segments.push(segment)
  }
  return `/${segments.join('/')}`
}

export function safePublicMediaURL(rawURL?: string): string | null {
  const safe = parseSafePublicURL(rawURL)
  if (!safe) return null
  const pathname = normalizedDecodedPathname(safe.parsed.pathname)
  if (!pathname || pathname.startsWith('/api/media/youtube/')) return null
  const parsed = safe.parsed
  return parsed.href
}

function normalizedVideoType(mediaType?: string): string {
  if (mediaType === 'youtube') return 'video/youtube'
  if (mediaType === 'bilibili') return 'video/bilibili'
  return mediaType ?? ''
}

function subscribeHref(source: string | null): string {
  if (!source) return '/login?intent=subscribe'
  const intent = parseAuthIntent(`?intent=subscribe&source=${encodeURIComponent(source)}`)
  return intent.kind === 'subscribe' ? `/login${authSearch(intent)}` : '/login?intent=subscribe'
}

function PublicMedia({ article, sourceURL }: Props & { sourceURL: string | null }) {
  if (!article.media_url) return null
  const mediaURL = safePublicMediaURL(article.media_url)
  const mediaType = normalizedVideoType(article.media_type)

  if (mediaURL && mediaType.startsWith('video/')) {
    const embed = parseStoredEmbedURL(mediaURL, mediaType)
    if (embed) return <div className="public-reader-media"><VideoEmbed {...embed} /></div>
  }

  if (mediaURL && mediaType.startsWith('audio/')) {
    return (
      <div className="public-reader-media card">
        <div className="public-reader-media-title">音频节目</div>
        <audio controls preload="none" src={mediaURL} />
        <div className="text-muted text-sm">{formatDuration(article.media_duration_seconds)}</div>
      </div>
    )
  }

  return (
    <div className="public-reader-media-fallback card">
      {sourceURL
        ? <a href={sourceURL} target="_blank" rel="noopener noreferrer">前往原网站播放</a>
        : <span>前往原网站播放</span>}
    </div>
  )
}

export default function PublicArticleReader({ article }: Props) {
  const published = formatDate(article.published_at)
  const sourceURL = safePublicSourceURL(article.url)
  return (
    <main className="public-reader">
      <header className="public-reader-brand">
        <span>RSS Pal</span>
        <span className="text-muted">· 分享文章</span>
      </header>

      <article>
        <section className="card public-reader-heading">
          <h1>{article.title}</h1>
          <div className="public-reader-meta text-muted text-sm">
            {article.feed_title && <span>{article.feed_title}</span>}
            {published && <time dateTime={article.published_at ?? undefined}>{published}</time>}
            <ReadingMeta wordCount={article.word_count} readingMinutes={article.reading_minutes} />
          </div>
        </section>

        {(article.summary_brief || article.summary_detailed) && (
          <section className="card public-reader-summary">
            <h2>AI 总结</h2>
            <div className="markdown-body">
              {article.summary_brief && <SummaryMarkdown source={article.summary_brief} externalLinksNewTab />}
              {article.summary_brief && article.summary_detailed && <hr />}
              {article.summary_detailed && <SummaryMarkdown source={article.summary_detailed} externalLinksNewTab />}
            </div>
          </section>
        )}

        {article.content && (
          <section className="card public-reader-content">
            <MarkdownArticle source={article.content} imageDimensions={article.image_dimensions} readOnly />
          </section>
        )}

        <PublicMedia article={article} sourceURL={sourceURL} />

        <section className="card public-reader-actions">
          {sourceURL
            ? (
              <a className="public-reader-primary-cta" href={sourceURL} target="_blank" rel="noopener noreferrer">
                阅读原文
              </a>
            )
            : <span className="text-muted">原文链接不可用</span>}
        </section>
      </article>

      <footer className="public-reader-footer">
        <span className="text-muted">由 RSS Pal 提供</span>
        <div className="public-reader-ctas">
          <a href={`/login${authSearch({ kind: 'use', returnTo: '/articles' })}`}>使用 RSS Pal</a>
          <a href={subscribeHref(sourceURL)}>订阅原始来源</a>
        </div>
      </footer>
    </main>
  )
}
