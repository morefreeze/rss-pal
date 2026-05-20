import { UserTag } from '../api/client'

// Source row aggregated from /api/clip items by EffectiveSource.key.
// `key` is what we pass to the API as the `source` filter; `title` is what
// we render. We can't precompute these from the feeds list because clip
// articles share one feed but split across many hosts.
export interface ClipSourceRow {
  key: string
  title: string
  count: number
}

// Clip page sidebar selection state. Tags are single-select: clicking a
// tag switches to it; clicking another swaps. There is no multi-select or
// AND/OR mode.
export type ClipSelection =
  | { kind: 'all' }
  | { kind: 'untagged' }
  | { kind: 'tag'; id: number }
  | { kind: 'source'; key: string; title: string }

interface Props {
  tags: UserTag[]
  sources: ClipSourceRow[]
  selection: ClipSelection
  onSelect: (sel: ClipSelection) => void
}

function isSourceActive(sel: ClipSelection, key: string): boolean {
  return sel.kind === 'source' && sel.key === key
}

export default function ClipTagSidebar({
  tags,
  sources,
  selection,
  onSelect,
}: Props) {
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
        </button>
        <button
          type="button"
          className={'saved-row' + (selection.kind === 'untagged' ? ' active' : '')}
          onClick={() => onSelect({ kind: 'untagged' })}
        >
          <span className="saved-row-label">(无 tag)</span>
        </button>
      </div>

      <div style={{ marginTop: 12 }}>
        <div className="saved-section-title">Tags</div>
        {tags.length === 0 ? (
          <div className="text-muted text-sm" style={{ padding: '4px 8px' }}>
            暂无 tag
          </div>
        ) : (
          tags.map(t => {
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

      <div style={{ marginTop: 12 }}>
        <div className="saved-section-title">Sources</div>
        {sources.length === 0 ? (
          <div className="text-muted text-sm" style={{ padding: '4px 8px' }}>
            暂无来源
          </div>
        ) : (
          sources.map(s => (
            <button
              key={s.key}
              type="button"
              className={'saved-row' + (isSourceActive(selection, s.key) ? ' active' : '')}
              onClick={() => onSelect({ kind: 'source', key: s.key, title: s.title })}
              title={s.title}
            >
              <span className="saved-row-label">{s.title}</span>
              <span className="saved-row-count">{s.count}</span>
            </button>
          ))
        )}
      </div>
    </aside>
  )
}
