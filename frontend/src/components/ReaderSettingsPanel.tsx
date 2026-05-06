import { useEffect, useRef, useState } from 'react'
import type {
  ReaderBgTheme,
  ReaderFontFamily,
} from '../hooks/useReaderSettings'

type Props = {
  fontSize: number
  fontFamily: ReaderFontFamily
  bgTheme: ReaderBgTheme
  onFontSize: (n: number) => void
  onFontFamily: (f: ReaderFontFamily) => void
  onBgTheme: (b: ReaderBgTheme) => void
}

const THEMES: { key: ReaderBgTheme; label: string; swatch: string }[] = [
  { key: 'default', label: '白',     swatch: '#ffffff' },
  { key: 'sepia',   label: '米黄',   swatch: '#f5edd6' },
  { key: 'green',   label: '护眼绿', swatch: '#cce8cf' },
  { key: 'gray',    label: '浅灰',   swatch: '#ebebeb' },
  { key: 'dark',    label: '暗色',   swatch: '#1a1a1a' },
]

export default function ReaderSettingsPanel({
  fontSize,
  fontFamily,
  bgTheme,
  onFontSize,
  onFontFamily,
  onBgTheme,
}: Props) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  // Close on outside click + Esc
  useEffect(() => {
    if (!open) return
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', onClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={ref} style={{ position: 'fixed', right: 24, bottom: 24, zIndex: 1100 }}>
      {open && (
        <div style={{
          position: 'absolute',
          right: 0,
          bottom: 64,
          width: 240,
          background: '#fff',
          color: '#1a1a1a',
          border: '1px solid #ddd',
          borderRadius: 10,
          padding: 14,
          boxShadow: '0 8px 24px rgba(0,0,0,0.12)',
        }}>
          <div style={{ fontSize: 11, color: '#888', marginBottom: 6 }}>字号</div>
          <div style={{ display: 'flex', gap: 6, marginBottom: 14 }}>
            <button onClick={() => onFontSize(fontSize - 1)} disabled={fontSize <= 12} style={fsBtn}>A−</button>
            <div style={{ flex: 2, padding: 6, textAlign: 'center', background: '#fff', border: '1px solid #ddd', borderRadius: 4, fontSize: 12 }}>
              {fontSize} px
            </div>
            <button onClick={() => onFontSize(fontSize + 1)} disabled={fontSize >= 24} style={{ ...fsBtn, fontSize: 14 }}>A+</button>
          </div>

          <div style={{ fontSize: 11, color: '#888', marginBottom: 6 }}>字体</div>
          <div style={{ display: 'flex', gap: 6, marginBottom: 14 }}>
            <button onClick={() => onFontFamily('sans')}
              style={{ ...familyBtn, ...(fontFamily === 'sans' ? familyBtnActive : {}) }}>Sans</button>
            <button onClick={() => onFontFamily('serif')}
              style={{ ...familyBtn, ...(fontFamily === 'serif' ? familyBtnActive : {}), fontFamily: '"Source Han Serif SC", "Songti SC", serif' }}>
              Serif
            </button>
          </div>

          <div style={{ fontSize: 11, color: '#888', marginBottom: 6 }}>背景</div>
          <div style={{ display: 'flex', gap: 8 }}>
            {THEMES.map(t => (
              <button key={t.key}
                aria-label={t.label}
                title={t.label}
                onClick={() => onBgTheme(t.key)}
                style={{
                  width: 28, height: 28, borderRadius: '50%',
                  background: t.swatch,
                  border: bgTheme === t.key ? '2px solid #222' : '1px solid #ccc',
                  padding: 0, cursor: 'pointer',
                }}
              />
            ))}
          </div>
        </div>
      )}

      <button
        onClick={() => setOpen(o => !o)}
        aria-label="阅读设置"
        title="阅读设置"
        style={{
          width: 48, height: 48, borderRadius: '50%',
          background: '#222', color: '#fff', fontWeight: 600,
          border: 'none', cursor: 'pointer',
          boxShadow: '0 4px 12px rgba(0,0,0,0.2)',
        }}
      >Aa</button>
    </div>
  )
}

const fsBtn: React.CSSProperties = {
  flex: 1, padding: 6, textAlign: 'center', background: '#f3f3f3', border: 'none', borderRadius: 4, fontSize: 12, cursor: 'pointer',
}
const familyBtn: React.CSSProperties = {
  flex: 1, padding: 8, textAlign: 'center', background: '#f3f3f3', border: '1px solid transparent', borderRadius: 4, fontSize: 13, cursor: 'pointer',
}
const familyBtnActive: React.CSSProperties = { background: '#fff', border: '1px solid #222' }
