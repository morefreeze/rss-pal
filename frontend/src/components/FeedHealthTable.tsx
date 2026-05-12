import { useState, useMemo } from 'react'
import { FeedHealthRow, updateFeedStatus, updateFeedWeight } from '../api/client'

type SortKey = 'feed_title' | 'produced' | 'ctr' | 'read_completion' | 'avg_duration_min' | 'last_active_at' | 'value_score'

interface Props {
  rows: FeedHealthRow[]
  onChange: () => void
}

const numCell: React.CSSProperties = { textAlign: 'right', padding: '6px 8px', whiteSpace: 'nowrap' }
const labelCell: React.CSSProperties = { padding: '6px 8px' }
const headerStyle: React.CSSProperties = { padding: '8px', borderBottom: '2px solid var(--border)', textAlign: 'left', cursor: 'pointer', userSelect: 'none' }

function pct(v: number | null): string {
  if (v == null) return '—'
  return (v * 100).toFixed(1) + '%'
}

function score(v: number | null): string {
  if (v == null) return '样本不足'
  return v.toFixed(2)
}

function relativeTime(iso: string | null): string {
  if (!iso) return '从未'
  const t = new Date(iso).getTime()
  const days = Math.floor((Date.now() - t) / (24 * 3600 * 1000))
  if (days === 0) return '今天'
  if (days === 1) return '昨天'
  if (days < 30) return `${days} 天前`
  if (days < 90) return `${Math.floor(days / 7)} 周前`
  return `${Math.floor(days / 30)} 个月前`
}

export default function FeedHealthTable({ rows, onChange }: Props) {
  const [sortKey, setSortKey] = useState<SortKey>('value_score')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')

  const sorted = useMemo(() => {
    const out = [...rows]
    out.sort((a, b) => {
      const av = a[sortKey] as any
      const bv = b[sortKey] as any
      // null/undefined sort to end regardless of dir
      if (av == null && bv == null) return 0
      if (av == null) return 1
      if (bv == null) return -1
      if (typeof av === 'string') {
        return sortDir === 'asc' ? av.localeCompare(bv) : bv.localeCompare(av)
      }
      return sortDir === 'asc' ? av - bv : bv - av
    })
    return out
  }, [rows, sortKey, sortDir])

  const headerClick = (k: SortKey) => {
    if (sortKey === k) setSortDir(sortDir === 'asc' ? 'desc' : 'asc')
    else { setSortKey(k); setSortDir('desc') }
  }

  const handleStatus = async (feedId: number, status: 'paused' | 'archived') => {
    if (!confirm(`确认${status === 'paused' ? '暂停' : '归档'}该 feed？`)) return
    await updateFeedStatus(feedId, status)
    onChange()
  }

  const handleWeight = async (feedId: number) => {
    const input = prompt('输入新的优先级权重（0.0 - 2.0，默认 1.0，降权常用 0.5）', '0.5')
    if (input == null) return
    const v = parseFloat(input)
    if (isNaN(v) || v < 0 || v > 2) { alert('值必须在 0 到 2 之间'); return }
    await updateFeedWeight(feedId, v)
    onChange()
  }

  return (
    <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
      <thead>
        <tr>
          <th style={headerStyle} onClick={() => headerClick('feed_title')}>Feed</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('produced')}>产出</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('ctr')}>CTR</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('read_completion')}>完读率</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('avg_duration_min')}>平均时长</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('last_active_at')}>最近</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('value_score')}>价值得分</th>
          <th style={headerStyle}>权重</th>
          <th style={headerStyle}>动作</th>
          <th style={headerStyle}>⚠</th>
        </tr>
      </thead>
      <tbody>
        {sorted.map(r => (
          <tr key={r.feed_id} style={{ borderBottom: '1px solid var(--border)' }}>
            <td style={labelCell}>
              {r.feed_title}
              {r.status !== 'active' && (
                <span style={{ marginLeft: 6, fontSize: 11, color: 'var(--fg-muted)' }}>[{r.status}]</span>
              )}
            </td>
            <td style={numCell}>{r.produced}</td>
            <td style={numCell}>{pct(r.ctr)}</td>
            <td style={numCell}>{pct(r.read_completion)}</td>
            <td style={numCell}>{r.avg_duration_min > 0 ? `${r.avg_duration_min.toFixed(1)} 分` : '—'}</td>
            <td style={numCell}>{relativeTime(r.last_active_at)}</td>
            <td style={numCell}>{score(r.value_score)}</td>
            <td style={numCell}>{r.priority_weight.toFixed(2)}</td>
            <td style={labelCell}>
              <button className="btn-ghost btn-sm" onClick={() => handleStatus(r.feed_id, 'paused')} disabled={r.status !== 'active'}>暂停</button>
              <button className="btn-ghost btn-sm" onClick={() => handleStatus(r.feed_id, 'archived')} style={{ marginLeft: 4 }}>归档</button>
              <button className="btn-ghost btn-sm" onClick={() => handleWeight(r.feed_id)} style={{ marginLeft: 4 }}>降权</button>
            </td>
            <td style={labelCell}>
              {r.pruning_rule && (
                <span title={r.pruning_rule.reason} style={{ color: '#c33', cursor: 'help' }}>
                  ⚠ {r.pruning_rule.label}
                </span>
              )}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
