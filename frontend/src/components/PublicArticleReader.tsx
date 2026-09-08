import MarkdownArticle from './MarkdownArticle'
import ReadingMeta from './ReadingMeta'
import SummaryMarkdown from './SummaryMarkdown'
import VideoEmbed from './VideoEmbed'
import { parseStoredEmbedURL } from './parseVideoPlaceholder'

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

export function publicMediaURL(rawURL?: string): string | null {
  if (!rawURL || rawURL.startsWith('//')) return null
  let parsed: URL
  try {
    parsed = new URL(rawURL)
  } catch {
    return null
  }
  if (!['http:', 'https:'].includes(parsed.protocol)) return null
  if (parsed.username || parsed.password) return null
  return parsed.href
}

function normalizedVideoType(mediaType?: string): string {
  if (mediaType === 'youtube') return 'video/youtube'
  if (mediaType === 'bilibili') return 'video/bilibili'
  return mediaType ?? ''
}

function subscribeHref(source: string): string {
  if (source.length > 2048) return '/login?intent=subscribe'
  try {
    const parsed = new URL(source)
    if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password) {
      return '/login?intent=subscribe'
    }
  } catch {
    return '/login?intent=subscribe'
  }
  return `/login?intent=subscribe&source=${encodeURIComponent(source)}`
}

function PublicMedia({ article }: Props) {
  if (!article.media_url) return null
  const mediaURL = publicMediaURL(article.media_url)
  const mediaType = normalizedVideoType(article.media_type)

  if (mediaURL && mediaType.startsWith('video/')) {
    const embed = parseStoredEmbedURL(mediaURL, mediaType)
    if (embed) return <div className="public-reader-media"><VideoEmbed {...embed} /></div>
  }

  if (mediaURL && mediaType.startsWith('audio/')) {
    return (
      <div className="public-reader-media card">
        <div className="public-reader-media-title">音频节目</div>
        <audio controls preload="metadata" src={mediaURL} />
        <div className="text-muted text-sm">{formatDuration(article.media_duration_seconds)}</div>
      </div>
    )
  }

  return (
    <div className="public-reader-media-fallback card">
      <a href={article.url} target="_blank" rel="noopener noreferrer">前往原网站播放</a>
    </div>
  )
}

export default function PublicArticleReader({ article }: Props) {
  const published = formatDate(article.published_at)
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
              {article.summary_brief && <SummaryMarkdown source={article.summary_brief} />}
              {article.summary_brief && article.summary_detailed && <hr />}
              {article.summary_detailed && <SummaryMarkdown source={article.summary_detailed} />}
            </div>
          </section>
        )}

        {article.content && (
          <section className="card public-reader-content">
            <MarkdownArticle source={article.content} imageDimensions={article.image_dimensions} readOnly />
          </section>
        )}

        <PublicMedia article={article} />

        <section className="card public-reader-actions">
          <a className="public-reader-primary-cta" href={article.url} target="_blank" rel="noopener noreferrer">
            阅读原文
          </a>
        </section>
      </article>

      <footer className="public-reader-footer">
        <span className="text-muted">由 RSS Pal 提供</span>
        <div className="public-reader-ctas">
          <a href="/login?intent=use">使用 RSS Pal</a>
          <a href={subscribeHref(article.url)}>订阅原始来源</a>
        </div>
      </footer>
    </main>
  )
}
