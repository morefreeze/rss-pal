import { useState } from 'react'
import { FeedHealthRow, updateFeedStatus, updateFeedWeight } from '../api/client'

interface Props {
  rows: FeedHealthRow[]
  onChange: () => void
}

export default function PruningDrawer({ rows, onChange }: Props) {
  const [open, setOpen] = useState(false)
  const [hidden, setHidden] = useState<Set<number>>(new Set())

  const candidates = rows.filter(r => r.pruning_rule && !hidden.has(r.feed_id))
  if (candidates.length === 0) return null

  const action = async (feedId: number, kind: '归档' | '暂停' | '降权') => {
    if (kind === '归档') await updateFeedStatus(feedId, 'archived')
    else if (kind === '暂停') await updateFeedStatus(feedId, 'paused')
    else await updateFeedWeight(feedId, 0.5)
    onChange()
  }

  const dismiss = (feedId: number) => {
    setHidden(prev => new Set(prev).add(feedId))
  }

  return (
    <div style={{ marginBottom: 16, border: '1px solid #fab', borderRadius: 8, background: '#fff5f5' }}>
      <div
        onClick={() => setOpen(o => !o)}
        style={{ padding: '12px 16px', cursor: 'pointer', display: 'flex', justifyContent: 'space-between' }}
      >
        <strong>⚠ {candidates.length} 个 feed 建议处理</strong>
        <span>{open ? '收起' : '展开'} ▾</span>
      </div>
      {open && (
        <div style={{ padding: '0 16px 12px' }}>
          {candidates.map(r => (
            <div key={r.feed_id} style={{ padding: '10px 0', borderTop: '1px dashed #fab' }}>
              <div>
                <strong>{r.feed_title}</strong>
                <span style={{ marginLeft: 8, padding: '2px 8px', background: '#c33', color: '#fff', borderRadius: 4, fontSize: 11 }}>
                  {r.pruning_rule!.label}
                </span>
              </div>
              <div style={{ fontSize: 13, color: '#666', margin: '4px 0' }}>
                原因：{r.pruning_rule!.reason}
              </div>
              <div>
                {r.pruning_rule!.suggested_actions.map(a => (
                  <button key={a} onClick={() => action(r.feed_id, a as any)} style={{ marginRight: 6 }}>
                    {a}
                  </button>
                ))}
                <button onClick={() => dismiss(r.feed_id)} style={{ marginRight: 6, color: '#888' }}>
                  暂不处理
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
