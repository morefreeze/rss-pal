import { TagSidebarData } from '../api/client'

export type TagFilter =
  | { kind: 'all' }
  | { kind: 'untagged' }
  | { kind: 'tag'; id: number }

interface Props {
  data: TagSidebarData
  selection: TagFilter
  onSelect: (sel: TagFilter) => void
}

export default function TagSidebar({ data, selection, onSelect }: Props) {
  return (
    <aside
      style={{
        width: 220,
        flexShrink: 0,
        borderRight: '1px solid var(--border)',
        padding: 12,
        overflowY: 'auto',
      }}
    >
      <div>
        <button
          type="button"
          className={'saved-row' + (selection.kind === 'all' ? ' active' : '')}
          onClick={() => onSelect({ kind: 'all' })}
        >
          <span className="saved-row-label">全部</span>
          <span className="saved-row-count">{data.total_count}</span>
        </button>
        <button
          type="button"
          className={'saved-row' + (selection.kind === 'untagged' ? ' active' : '')}
          onClick={() => onSelect({ kind: 'untagged' })}
        >
          <span className="saved-row-label">(无 tag)</span>
          <span className="saved-row-count">{data.untagged_count}</span>
        </button>
      </div>

      <div style={{ marginTop: 12 }}>
        <div className="saved-section-title">Tags</div>
        {data.tags.length === 0 ? (
          <div className="text-muted text-sm" style={{ padding: '4px 8px' }}>
            暂无 tag
          </div>
        ) : (
          data.tags.map(t => {
            const active = selection.kind === 'tag' && selection.id === t.id
            return (
              <button
                key={t.id}
                type="button"
                className={'saved-row' + (active ? ' active' : '')}
                onClick={() => onSelect({ kind: 'tag', id: t.id })}
              >
                <span className="saved-row-label">{t.name}</span>
                <span className="saved-row-count">{t.article_count}</span>
              </button>
            )
          })
        )}
      </div>
    </aside>
  )
}
