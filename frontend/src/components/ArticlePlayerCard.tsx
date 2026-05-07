import { Article } from '../api/client'
import { usePlayer } from '../player/PlayerContext'
import Spinner from './Spinner'

function fmtMinSec(sec: number): string {
  if (!sec || sec <= 0) return ''
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return `${m}分${s.toString().padStart(2, '0')}秒`
}

export default function ArticlePlayerCard({ article }: { article: Article }) {
  const p = usePlayer()
  if (!article.media_url) return null

  const isCurrent = p.articleId === article.id
  const playing = isCurrent && p.playing

  return (
    <div
      style={{
        margin: '12px 0 20px',
        padding: 16,
        border: '1px solid #ddd',
        borderRadius: 8,
        background: '#fafafa',
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
          background: '#0066cc',
          color: '#fff',
          border: 'none',
          fontSize: 24,
          cursor: 'pointer',
          flexShrink: 0,
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        {isCurrent && p.loading ? <Spinner size={24} color="#fff" /> : playing ? '⏸' : '▶'}
      </button>
      <div style={{ flex: 1 }}>
        <div style={{ fontWeight: 600, fontSize: 15 }}>音频节目</div>
        <div style={{ fontSize: 13, color: '#666' }}>
          {fmtMinSec(article.media_duration_seconds || 0) || '时长未知'}
        </div>
      </div>
    </div>
  )
}
