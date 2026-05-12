import { Article } from '../api/client'
import { usePlayer } from '../player/PlayerContext'
import Spinner from './Spinner'
import VideoEmbed from './VideoEmbed'
import { parseStoredEmbedURL } from './parseVideoPlaceholder'

function fmtMinSec(sec: number): string {
  if (!sec || sec <= 0) return ''
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${m}分${s.toString().padStart(2, '0')}秒`
}

export default function ArticlePlayerCard({ article }: { article: Article }) {
  if (!article.media_url) return null

  // Branch on media_type: video → embedded iframe; otherwise → audio player.
  if (article.media_type && article.media_type.startsWith('video/')) {
    const v = parseStoredEmbedURL(article.media_url, article.media_type)
    if (!v) return null
    return <VideoEmbed {...v} />
  }

  return <AudioCard article={article} />
}

function AudioCard({ article }: { article: Article }) {
  const p = usePlayer()
  const isCurrent = p.articleId === article.id
  const playing = isCurrent && p.playing

  return (
    <div
      style={{
        margin: '12px 0 20px',
        padding: 16,
        border: '1px solid var(--border)',
        borderRadius: 8,
        background: 'var(--surface)',
        display: 'flex',
        alignItems: 'center',
        gap: 16,
      }}
    >
      <button
        onClick={() => (isCurrent ? p.toggle() : p.playArticle(article))}
        aria-label={isCurrent && p.loading ? '加载中' : playing ? '暂停' : '播放'}
        disabled={isCurrent && p.loading && !playing}
        style={{
          width: 56,
          height: 56,
          borderRadius: 999,
          flexShrink: 0,
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: 24,
        }}
      >
        {isCurrent && p.loading ? <Spinner size={24} color="var(--accent-fg)" /> : playing ? '⏸' : '▶'}
      </button>
      <div style={{ flex: 1 }}>
        <div style={{ fontWeight: 600, fontSize: 15 }}>音频节目</div>
        <div style={{ fontSize: 13, color: 'var(--fg-muted)' }}>
          {fmtMinSec(article.media_duration_seconds || 0) || '时长未知'}
        </div>
      </div>
    </div>
  )
}
