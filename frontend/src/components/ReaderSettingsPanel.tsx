import { useEffect, useRef, useState } from 'react'
import type { ReaderFontFamily } from '../hooks/useReaderSettings'
import { useBreakpoint } from '../hooks/useBreakpoint'

type Props = {
  fontSize: number
  fontFamily: ReaderFontFamily
  onFontSize: (n: number) => void
  onFontFamily: (f: ReaderFontFamily) => void
}

export default function ReaderSettingsPanel({
  fontSize, fontFamily, onFontSize, onFontFamily,
}: Props) {
  const [open, setOpen] = useState(false)
  const bp = useBreakpoint()
  const ref = useRef<HTMLDivElement>(null)

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

  const body = (
    <>
      <div className="text-sm text-muted" style={{ marginBottom: 6 }}>字号</div>
      <div className="flex gap-2" style={{ marginBottom: 14 }}>
        <button className="btn-ghost btn-sm" onClick={() => onFontSize(fontSize - 1)} disabled={fontSize <= 12} style={{ flex: 1 }}>A−</button>
        <div style={{ flex: 2, padding: 4, textAlign: 'center', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12 }}>
          {fontSize} px
        </div>
        <button className="btn-ghost btn-sm" onClick={() => onFontSize(fontSize + 1)} disabled={fontSize >= 24} style={{ flex: 1, fontSize: 14 }}>A+</button>
      </div>

      <div className="text-sm text-muted" style={{ marginBottom: 6 }}>字体</div>
      <div className="flex gap-2">
        <button
          className={fontFamily === 'sans' ? 'btn-sm' : 'btn-ghost btn-sm'}
          onClick={() => onFontFamily('sans')}
          style={{ flex: 1 }}
        >Sans</button>
        <button
          className={fontFamily === 'serif' ? 'btn-sm' : 'btn-ghost btn-sm'}
          onClick={() => onFontFamily('serif')}
          style={{ flex: 1, fontFamily: 'var(--font-serif)' }}
        >Serif</button>
      </div>
    </>
  )

  const fab = (
    <button
      onClick={() => setOpen(o => !o)}
      aria-label="阅读设置"
      title="阅读设置"
      style={{ width: 48, height: 48, borderRadius: '50%', padding: 0, fontWeight: 700 }}
    >Aa</button>
  )

  if (bp === 'phone') {
    return (
      <div ref={ref} style={{
        position: 'fixed',
        right: 16,
        bottom: 'calc(56px + env(safe-area-inset-bottom) + 16px)',
        zIndex: 1100,
      }}>
        {fab}
        {open && (
          <>
            <div
              onClick={() => setOpen(false)}
              style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.35)', zIndex: 1199 }}
            />
            <div
              role="dialog"
              aria-label="阅读设置"
              style={{
                position: 'fixed',
                left: 0, right: 0, bottom: 0,
                background: 'var(--surface)',
                borderTop: '1px solid var(--border)',
                borderTopLeftRadius: 16,
                borderTopRightRadius: 16,
                padding: '8px 16px calc(env(safe-area-inset-bottom) + 16px)',
                zIndex: 1200,
                boxShadow: '0 -4px 16px rgba(0,0,0,0.18)',
              }}
            >
              <div
                aria-hidden="true"
                style={{
                  width: 36, height: 4, borderRadius: 2,
                  background: 'var(--border)',
                  margin: '8px auto 14px',
                }}
              />
              {body}
            </div>
          </>
        )}
      </div>
    )
  }

  return (
    <div ref={ref} style={{ position: 'fixed', right: 24, bottom: 24, zIndex: 1100 }}>
      {open && (
        <div style={{
          position: 'absolute',
          right: 0,
          bottom: 64,
          width: 240,
          background: 'var(--surface)',
          color: 'var(--fg)',
          border: '1px solid var(--border)',
          borderRadius: 10,
          padding: 14,
          boxShadow: '0 8px 24px rgba(0,0,0,0.18)',
        }}>
          {body}
        </div>
      )}
      {fab}
    </div>
  )
}
