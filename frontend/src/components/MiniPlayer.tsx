import { usePlayer } from '../player/PlayerContext'

const SPEEDS = [1, 1.25, 1.5, 1.75, 2] as const

function fmt(sec: number): string {
  if (!isFinite(sec) || sec < 0) return '--:--'
  const total = Math.floor(sec)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
}

export default function MiniPlayer() {
  const p = usePlayer()
  if (p.articleId === null) return null

  return (
    <div
      role="region"
      aria-label="Podcast player"
      style={{
        position: 'fixed',
        bottom: 0,
        left: 0,
        right: 0,
        height: 64,
        background: '#fff',
        borderTop: '1px solid #ddd',
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        padding: '0 12px',
        boxShadow: '0 -2px 8px rgba(0,0,0,0.08)',
        zIndex: 1000,
      }}
    >
      <button onClick={p.toggle} aria-label={p.playing ? '暂停' : '播放'} style={{ fontSize: 20, padding: '4px 10px' }}>
        {p.playing ? '⏸' : '▶'}
      </button>
      <button onClick={() => p.skip(-5)} aria-label="后退5秒" style={{ padding: '4px 8px' }}>⏪5</button>
      <button onClick={() => p.skip(10)} aria-label="前进10秒" style={{ padding: '4px 8px' }}>⏩10</button>

      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        <div style={{ fontSize: 13, fontWeight: 600, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {p.title}
          {p.feedTitle && <span style={{ color: '#888', fontWeight: 400 }}> · {p.feedTitle}</span>}
        </div>
        <input
          type="range"
          min={0}
          max={p.duration || 0}
          value={p.position}
          onChange={e => p.seek(Number(e.target.value))}
          style={{ width: '100%' }}
          aria-label="播放进度"
        />
      </div>

      <span style={{ fontSize: 12, color: '#666', whiteSpace: 'nowrap' }}>
        {fmt(p.position)} / {fmt(p.duration)}
      </span>

      <select
        value={p.speed}
        onChange={e => p.setSpeed(Number(e.target.value) as 1 | 1.25 | 1.5 | 1.75 | 2)}
        aria-label="播放速度"
        style={{ fontSize: 13 }}
      >
        {SPEEDS.map(s => <option key={s} value={s}>{s}x</option>)}
      </select>

      <button onClick={p.close} aria-label="关闭播放器" style={{ padding: '4px 8px' }}>✕</button>

      {p.error && <span style={{ color: '#c00', fontSize: 12 }}>{p.error}</span>}
    </div>
  )
}
