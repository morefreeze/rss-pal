import { useEffect, useMemo, useRef, useState } from 'react'
import { getBriefingIndex, BriefingIndex } from '../api/client'

interface Props {
  currentWeekStart: string             // YYYY-MM-DD (Monday Asia/Shanghai)
  onPick: (weekStart: string) => void
}

type Status = 'done' | 'pending' | 'disabled' | 'today'

function pad(n: number): string {
  return n < 10 ? '0' + n : '' + n
}
function ymd(d: Date): string {
  return d.getUTCFullYear() + '-' + pad(d.getUTCMonth() + 1) + '-' + pad(d.getUTCDate())
}
function parseMondayUTC(s: string): Date {
  return new Date(s + 'T00:00:00+08:00')
}

function monthRelativeWeekNumber(weekStart: string): number {
  const d = parseMondayUTC(weekStart)
  const firstOfMonth = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1, -8))
  let dow = firstOfMonth.getUTCDay()
  if (dow === 0) dow = 7
  const offsetToFirstMonday = dow === 1 ? 0 : (8 - dow)
  const firstMonday = new Date(firstOfMonth)
  firstMonday.setUTCDate(firstOfMonth.getUTCDate() + offsetToFirstMonday)
  const diffDays = Math.round((d.getTime() - firstMonday.getTime()) / 86400000)
  return Math.floor(diffDays / 7) + 1
}

function rangeText(weekStart: string): string {
  const start = parseMondayUTC(weekStart)
  const end = new Date(start); end.setUTCDate(start.getUTCDate() + 6)
  return pad(start.getUTCMonth() + 1) + '.' + pad(start.getUTCDate())
    + '-' + pad(end.getUTCMonth() + 1) + '.' + pad(end.getUTCDate())
}

function classify(ws: string, idx: BriefingIndex | null): Status {
  if (!idx || !idx.this_week_start) return 'disabled'
  if (ws === idx.this_week_start) return 'today'
  if (idx.cached.includes(ws)) return 'done'
  if (ws >= idx.pending_window_start && ws < idx.this_week_start) return 'pending'
  return 'disabled'
}

export default function BriefingWCardStrip({ currentWeekStart, onPick }: Props) {
  const [index, setIndex] = useState<BriefingIndex | null>(null)
  const stripRef = useRef<HTMLDivElement>(null)

  const anchor = index?.this_week_start ?? currentWeekStart
  const weeks = useMemo(() => {
    const anchorD = parseMondayUTC(anchor)
    const out: string[] = []
    for (let i = 7; i >= 0; i--) {
      const d = new Date(anchorD); d.setUTCDate(anchorD.getUTCDate() - i * 7)
      out.push(ymd(d))
    }
    return out
  }, [anchor])

  useEffect(() => {
    if (weeks.length === 0) return
    const from = weeks[0]
    const to = parseMondayUTC(weeks[weeks.length - 1])
    to.setUTCDate(to.getUTCDate() + 6)
    getBriefingIndex('weekly', from, ymd(to))
      .then(setIndex)
      .catch(() => { /* leave uncolored */ })
  }, [weeks])

  useEffect(() => {
    const el = stripRef.current
    if (el) el.scrollLeft = el.scrollWidth
  }, [weeks])

  return (
    <div
      ref={stripRef}
      style={{
        display: 'flex',
        gap: 8,
        overflowX: 'auto',
        paddingBottom: 8,
        marginBottom: 16,
      }}
    >
      {weeks.map(ws => {
        const status = classify(ws, index)
        const disabled = status === 'disabled'
        const isCurrent = ws === currentWeekStart
        return (
          <button
            type="button"
            key={ws}
            disabled={disabled}
            aria-disabled={disabled}
            aria-current={isCurrent ? 'true' : undefined}
            data-status={status}
            onClick={() => { if (!disabled) onPick(ws) }}
            style={{
              flex: '0 0 auto',
              width: 88,
              padding: '8px 6px',
              border: isCurrent ? '2px solid var(--accent)' : '1px solid transparent',
              borderRadius: 8,
              background:
                status === 'done' ? 'var(--cal-done)' :
                status === 'pending' ? 'var(--cal-pending)' :
                status === 'today' ? 'var(--cal-today)' :
                'var(--cal-disabled)',
              color:
                status === 'done' || status === 'pending' || status === 'today' ? '#fff' :
                'var(--fg)',
              opacity: status === 'disabled' ? 0.45 : 1,
              cursor: disabled ? 'not-allowed' : 'pointer',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 18, fontWeight: 700, lineHeight: 1 }}>
              W{monthRelativeWeekNumber(ws)}
            </div>
            <div style={{ fontSize: 11, marginTop: 4, opacity: 0.9 }}>
              {rangeText(ws)}
            </div>
          </button>
        )
      })}
    </div>
  )
}
