import { UserTag } from '../api/client'

// Source row aggregated from /api/saved items by EffectiveSource.key.
// `key` is what we pass to the API as the `source` filter; `title` is what
// we render. We can't precompute these from the feeds list because saved
// articles share one feed but split across many hosts.
export interface SavedSourceRow {
  key: string
  title: string
  count: number
}

// Saved page sidebar selection state. The 'tag' variant carries an array
// of ids + a mode so the same shape covers both single and multi select.
export type SavedSelection =
  | { kind: 'all' }
  | { kind: 'untagged' }
  | { kind: 'tag'; ids: number[]; mode: 'and' | 'or' }
  | { kind: 'source'; key: string; title: string }

interface Props {
  tags: UserTag[]
  sources: SavedSourceRow[]
  selection: SavedSelection
  onSelect: (sel: SavedSelection) => void
  multi: boolean
  onToggleMulti: () => void
  onToggleTag: (tagId: number) => void
  onModeChange: (mode: 'and' | 'or') => void
}

function isTagActive(sel: SavedSelection, tagId: number): boolean {
  return sel.kind === 'tag' && sel.ids.includes(tagId)
}

function isSourceActive(sel: SavedSelection, key: string): boolean {
  return sel.kind === 'source' && sel.key === key
}

export default function SavedTagSidebar({
  tags,
  sources,
  selection,
  onSelect,
  multi,
  onToggleMulti,
  onToggleTag,
  onModeChange,
}: Props) {
  const tagMode = selection.kind === 'tag' ? selection.mode : 'and'
  const tagSelectedCount = selection.kind === 'tag' ? selection.ids.length : 0

  const handleTagClick = (tagId: number) => {
    if (multi) onToggleTag(tagId)
    else onSelect({ kind: 'tag', ids: [tagId], mode: 'and' })
  }

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
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 8,
          }}
        >
          <div className="saved-section-title">Tags</div>
          <button
            type="button"
            onClick={onToggleMulti}
            className="secondary"
            style={{ fontSize: 11, padding: '2px 8px', margin: '0 0 4px 0' }}
            title={multi ? '关闭多选' : '开启多选'}
          >
            {multi ? '✓ 多选' : '多选'}
          </button>
        </div>
        {multi && tagSelectedCount > 1 && (
          <div style={{ marginBottom: 6 }}>
            <span className="saved-mode-toggle">
              <button
                type="button"
                className={tagMode === 'and' ? 'active' : ''}
                onClick={() => onModeChange('and')}
                title="同时包含全部 tag"
              >
                AND
              </button>
              <button
                type="button"
                className={tagMode === 'or' ? 'active' : ''}
                onClick={() => onModeChange('or')}
                title="包含任意 tag"
              >
                OR
              </button>
            </span>
          </div>
        )}
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
              onClick={() => handleTagClick(t.id)}
            >
              <span className="saved-row-label">{t.name}</span>
              <span className="saved-row-count">{t.article_count}</span>
            </button>
          ))
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
