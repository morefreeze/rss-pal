import { useEffect, useMemo, useState } from 'react'
import { getBriefingIndex, BriefingIndex } from '../api/client'

interface Props {
  currentDate: string                  // YYYY-MM-DD — the digest's shown_date
  onPick: (date: string) => void
  onClose: () => void
}

type CellStatus = 'done' | 'pending' | 'disabled' | 'today' | 'future'

function pad(n: number): string {
  return n < 10 ? '0' + n : '' + n
}

function ymd(d: Date): string {
  return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate())
}

function parseShanghai(s: string): Date {
  return new Date(s + 'T00:00:00+08:00')
}

function firstOfMonth(year: number, month0: number): Date {
  return new Date(Date.UTC(year, month0, 1, -8)) // 00:00 Shanghai
}

function classifyCell(d: string, idx: BriefingIndex | null): CellStatus {
  if (!idx || !idx.today_label) return 'disabled'
  if (d > idx.today_label) return 'future'
  if (d === idx.today_label) return 'today'
  if (idx.cached.includes(d)) return 'done'
  if (d >= idx.pending_window_start) return 'pending'
  return 'disabled'
}

const WEEKDAY_LABELS = ['一', '二', '三', '四', '五', '六', '日']

export default function BriefingCalendar({ currentDate, onPick, onClose }: Props) {
  const initialMonth = useMemo(() => {
    const d = parseShanghai(currentDate)
    return d.getUTCFullYear() + '-' + pad(d.getUTCMonth() + 1)
  }, [currentDate])

  const [month, setMonth] = useState(initialMonth)
  const [index, setIndex] = useState<BriefingIndex | null>(null)

  const [year, monthOneBased] = useMemo(() => {
    const [y, m] = month.split('-').map(Number)
    return [y, m]
  }, [month])
  const month0 = monthOneBased - 1

  useEffect(() => {
    const first = firstOfMonth(year, month0)
    const last = new Date(first)
    last.setUTCMonth(first.getUTCMonth() + 1)
    last.setUTCDate(0)
    const fromDate = new Date(first); fromDate.setUTCDate(first.getUTCDate() - 7)
    const toDate = new Date(last); toDate.setUTCDate(last.getUTCDate() + 7)
    getBriefingIndex('daily', ymd(fromDate), ymd(toDate))
      .then(setIndex)
      .catch(() => { /* leave cells uncolored on error */ })
  }, [year, month0])

  const grid = useMemo(() => {
    const first = firstOfMonth(year, month0)
    let firstDow = first.getUTCDay()
    if (firstDow === 0) firstDow = 7
    const gridStart = new Date(first)
    gridStart.setUTCDate(first.getUTCDate() - (firstDow - 1))
    const days: { date: string; inMonth: boolean }[] = []
    for (let i = 0; i < 42; i++) {
      const d = new Date(gridStart)
      d.setUTCDate(gridStart.getUTCDate() + i)
      days.push({
        date: ymd(d),
        inMonth: d.getUTCMonth() === month0,
      })
    }
    return days
  }, [year, month0])

  const shiftMonth = (delta: number) => {
    const next = new Date(Date.UTC(year, month0 + delta, 1, -8))
    setMonth(next.getUTCFullYear() + '-' + pad(next.getUTCMonth() + 1))
  }

  return (
    <div
      role="dialog"
      aria-label="选择日期"
      style={{
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        borderRadius: 8,
        boxShadow: '0 8px 24px rgba(0,0,0,0.18)',
        padding: 12,
        width: 280,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
        <button type="button" onClick={() => shiftMonth(-1)} aria-label="上一月" style={btnStyle}>‹</button>
        <div style={{ fontWeight: 600 }}>{year} 年 {monthOneBased} 月</div>
        <button type="button" onClick={() => shiftMonth(1)} aria-label="下一月" style={btnStyle}>›</button>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: 2, marginBottom: 6 }}>
        {WEEKDAY_LABELS.map(w => (
          <div key={w} style={{ textAlign: 'center', fontSize: 11, color: 'var(--fg-muted)', padding: '2px 0' }}>{w}</div>
        ))}
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: 2 }}>
        {grid.map(({ date, inMonth }) => {
          const status: CellStatus = inMonth ? classifyCell(date, index) : 'future'
          const disabled = !inMonth || status === 'disabled' || status === 'future'
          const isCurrent = date === currentDate
          return (
            <button
              type="button"
              key={date}
              disabled={disabled}
              aria-disabled={disabled}
              aria-current={isCurrent ? 'true' : undefined}
              onClick={() => { if (!disabled) onPick(date) }}
              data-status={inMonth ? status : 'out'}
              style={{
                height: 32,
                fontSize: 13,
                border: isCurrent ? '2px solid var(--accent)' : '1px solid transparent',
                borderRadius: 4,
                background:
                  !inMonth ? 'transparent' :
                  status === 'done' ? 'var(--cal-done)' :
                  status === 'pending' ? 'var(--cal-pending)' :
                  status === 'today' ? 'var(--cal-today)' :
                  'var(--cal-disabled)',
                color:
                  !inMonth ? 'var(--fg-muted)' :
                  status === 'done' || status === 'pending' || status === 'today' ? '#fff' :
                  'var(--fg)',
                opacity: !inMonth ? 0.45 : status === 'disabled' ? 0.45 : 1,
                cursor: disabled ? 'not-allowed' : 'pointer',
              }}
            >
              {Number(date.slice(-2))}
            </button>
          )
        })}
      </div>
      <div style={{ marginTop: 8, display: 'flex', justifyContent: 'flex-end' }}>
        <button type="button" onClick={onClose} style={btnStyle}>关闭</button>
      </div>
    </div>
  )
}

const btnStyle: React.CSSProperties = {
  background: 'transparent',
  border: '1px solid var(--border)',
  borderRadius: 4,
  padding: '4px 8px',
  cursor: 'pointer',
  color: 'var(--fg)',
}
