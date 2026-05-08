import { Feed, UserTag } from '../api/client'

// Saved page sidebar selection state. Single-select for Phase 12.
// Phase 13 will widen the 'tag' variant to multi-select with AND/OR mode.
export type SavedSelection =
  | { kind: 'all' }
  | { kind: 'untagged' }
  | { kind: 'tag'; id: number }
  | { kind: 'source'; feedId: number }

interface Props {
  tags: UserTag[]
  feeds: Feed[]
  selection: SavedSelection
  onSelect: (sel: SavedSelection) => void
}

function isTagActive(sel: SavedSelection, tagId: number): boolean {
  return sel.kind === 'tag' && sel.id === tagId
}

function isSourceActive(sel: SavedSelection, feedId: number): boolean {
  return sel.kind === 'source' && sel.feedId === feedId
}

export default function SavedTagSidebar({ tags, feeds, selection, onSelect }: Props) {
  return (
    <aside
      style={{
        width: 220,
        flexShrink: 0,
        borderRight: '1px solid #e2e8f0',
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
          tags.map(t => (
            <button
              key={t.id}
              type="button"
              className={'saved-row' + (isTagActive(selection, t.id) ? ' active' : '')}
              onClick={() => onSelect({ kind: 'tag', id: t.id })}
            >
              <span className="saved-row-label">{t.name}</span>
              <span className="saved-row-count">{t.article_count}</span>
            </button>
          ))
        )}
      </div>

      <div style={{ marginTop: 12 }}>
        <div className="saved-section-title">Sources</div>
        {feeds.length === 0 ? (
          <div className="text-muted text-sm" style={{ padding: '4px 8px' }}>
            暂无来源
          </div>
        ) : (
          feeds.map(f => (
            <button
              key={f.id}
              type="button"
              className={'saved-row' + (isSourceActive(selection, f.id) ? ' active' : '')}
              onClick={() => onSelect({ kind: 'source', feedId: f.id })}
              title={f.title || f.url}
            >
              <span className="saved-row-label">{f.title || f.url}</span>
            </button>
          ))
        )}
      </div>
    </aside>
  )
}
