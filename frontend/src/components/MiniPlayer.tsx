import { useEffect, useRef, useState } from 'react'
import { usePlayer } from '../player/PlayerContext'
import Spinner from './Spinner'
import { useBreakpoint } from '../hooks/useBreakpoint'

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
  const bp = useBreakpoint()
  const [dragValue, setDragValue] = useState<number | null>(null)
  const [extraOpen, setExtraOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const [narrow, setNarrow] = useState(false)

  useEffect(() => {
    if (!ref.current) return
    const el = ref.current
    const ro = new ResizeObserver(entries => {
      for (const entry of entries) {
        setNarrow(entry.contentRect.width < 480)
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [p.articleId])

  if (p.articleId === null) return null

  const sliderValue = dragValue ?? p.position
  const bottomOffset = bp === 'desktop'
    ? '0'
    : 'calc(56px + env(safe-area-inset-bottom))'

  return (
    <div
      ref={ref}
      role="region"
      aria-label="Podcast player"
      style={{
        position: 'fixed',
        bottom: bottomOffset,
        left: 0,
        right: 0,
        height: 64,
        background: 'var(--surface)',
        borderTop: '1px solid var(--border)',
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '0 12px',
        boxShadow: '0 -2px 8px rgba(0,0,0,0.08)',
        zIndex: 999,
      }}
    >
      <button
        onClick={p.toggle}
        aria-label={p.loading ? '加载中' : p.playing ? '暂停' : '播放'}
        disabled={p.loading && !p.playing}
        style={{ fontSize: 20, padding: '4px 10px', minWidth: 40, display: 'inline-flex', alignItems: 'center', justifyContent: 'center' }}
      >
        {p.loading ? <Spinner size={16} /> : p.playing ? '⏸' : '▶'}
      </button>

      {!narrow && (
        <>
          <button onClick={() => p.skip(-5)} aria-label="后退5秒" style={{ padding: '4px 8px' }}>⏪5</button>
          <button onClick={() => p.skip(10)} aria-label="前进10秒" style={{ padding: '4px 8px' }}>⏩10</button>
        </>
      )}

      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        <div style={{ fontSize: 13, fontWeight: 600, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {p.title}
          {p.feedTitle && <span style={{ color: 'var(--fg-muted)', fontWeight: 400 }}> · {p.feedTitle}</span>}
        </div>
        <input
          type="range"
          min={0}
          max={p.duration || 0}
          value={sliderValue}
          onChange={e => setDragValue(Number(e.target.value))}
          onMouseUp={() => {
            if (dragValue !== null) { p.seek(dragValue); setDragValue(null) }
          }}
          onTouchEnd={() => {
            if (dragValue !== null) { p.seek(dragValue); setDragValue(null) }
          }}
          onKeyUp={() => {
            if (dragValue !== null) { p.seek(dragValue); setDragValue(null) }
          }}
          style={{ width: '100%' }}
          aria-label="播放进度"
        />
      </div>

      {!narrow && (
        <span style={{ fontSize: 12, color: 'var(--fg-muted)', whiteSpace: 'nowrap' }}>
          {fmt(sliderValue)} / {fmt(p.duration)}
        </span>
      )}

      {!narrow && (
        <select
          value={p.speed}
          onChange={e => p.setSpeed(Number(e.target.value) as 1 | 1.25 | 1.5 | 1.75 | 2)}
          aria-label="播放速度"
          style={{ fontSize: 13 }}
        >
          {SPEEDS.map(s => <option key={s} value={s}>{s}x</option>)}
        </select>
      )}

      {narrow && (
        <button
          onClick={() => setExtraOpen(o => !o)}
          aria-label="更多控制"
          style={{ padding: '4px 8px' }}
        >⋯</button>
      )}

      <button onClick={p.close} aria-label="关闭播放器" style={{ padding: '4px 8px' }}>✕</button>

      {p.error && <span style={{ color: '#c00', fontSize: 12 }}>{p.error}</span>}

      {narrow && extraOpen && (
        <div
          style={{
            position: 'absolute',
            right: 8,
            bottom: 'calc(100% + 4px)',
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            padding: 8,
            display: 'flex',
            gap: 6,
            alignItems: 'center',
            boxShadow: '0 4px 12px rgba(0,0,0,0.18)',
            zIndex: 1001,
          }}
        >
          <button onClick={() => p.skip(-5)} style={{ padding: '4px 8px' }}>⏪5</button>
          <button onClick={() => p.skip(10)} style={{ padding: '4px 8px' }}>⏩10</button>
          <select
            value={p.speed}
            onChange={e => p.setSpeed(Number(e.target.value) as 1 | 1.25 | 1.5 | 1.75 | 2)}
            aria-label="播放速度"
            style={{ fontSize: 13 }}
          >
            {SPEEDS.map(s => <option key={s} value={s}>{s}x</option>)}
          </select>
          <span style={{ fontSize: 12, color: 'var(--fg-muted)', whiteSpace: 'nowrap' }}>
            {fmt(sliderValue)} / {fmt(p.duration)}
          </span>
        </div>
      )}
    </div>
  )
}
