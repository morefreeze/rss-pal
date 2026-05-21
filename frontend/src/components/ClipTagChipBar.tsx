import { UserTag } from '../api/client'
import { ClipSelection, ClipSourceRow } from './ClipTagSidebar'

interface Props {
  tags: UserTag[]
  sources: ClipSourceRow[]
  selection: ClipSelection
  onSelect: (sel: ClipSelection) => void
}

function isActive(sel: ClipSelection, target: ClipSelection): boolean {
  if (sel.kind !== target.kind) return false
  if (sel.kind === 'all' || sel.kind === 'untagged') return true
  if (sel.kind === 'tag' && target.kind === 'tag') return sel.id === target.id
  if (sel.kind === 'source' && target.kind === 'source') return sel.key === target.key
  return false
}

export default function ClipTagChipBar({ tags, sources, selection, onSelect }: Props) {
  return (
    <div
      style={{
        position: 'sticky',
        top: 0,
        zIndex: 50,
        background: 'var(--bg)',
        margin: '0 -12px 12px',
        padding: '8px 12px',
        borderBottom: '1px solid var(--border)',
        overflowX: 'auto',
        WebkitOverflowScrolling: 'touch',
      }}
    >
      <div style={{ display: 'flex', gap: 8, flexWrap: 'nowrap', minWidth: 'min-content' }}>
        <Chip active={isActive(selection, { kind: 'all' })} onClick={() => onSelect({ kind: 'all' })}>
          全部
        </Chip>
        <Chip active={isActive(selection, { kind: 'untagged' })} onClick={() => onSelect({ kind: 'untagged' })}>
          无 tag
        </Chip>
        {sources.length > 0 && <Divider />}
        {sources.map(s => (
          <Chip
            key={`src-${s.key}`}
            active={isActive(selection, { kind: 'source', key: s.key, title: s.title })}
            onClick={() => onSelect({ kind: 'source', key: s.key, title: s.title })}
            title={s.title}
          >
            {s.title}
            <span style={{ opacity: 0.6, marginLeft: 4, fontSize: 11 }}>{s.count}</span>
          </Chip>
        ))}
        {tags.length > 0 && <Divider />}
        {tags.map(t => (
          <Chip
            key={`tag-${t.id}`}
            active={isActive(selection, { kind: 'tag', id: t.id })}
            onClick={() => onSelect({ kind: 'tag', id: t.id })}
          >
            {t.name}
            <span style={{ opacity: 0.6, marginLeft: 4, fontSize: 11 }}>{t.article_count}</span>
          </Chip>
        ))}
      </div>
    </div>
  )
}

function Chip({
  active, onClick, children, title,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
  title?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      style={{
        flexShrink: 0,
        padding: '6px 12px',
        height: 'auto',
        minHeight: 32,
        borderRadius: 999,
        border: '1px solid ' + (active ? 'var(--accent)' : 'var(--border)'),
        background: active ? 'var(--accent-soft)' : 'var(--surface)',
        color: active ? 'var(--accent)' : 'var(--fg)',
        fontSize: 13,
        fontWeight: active ? 600 : 400,
        whiteSpace: 'nowrap',
      }}
    >
      {children}
    </button>
  )
}

function Divider() {
  return (
    <div
      aria-hidden="true"
      style={{
        flexShrink: 0,
        width: 1,
        margin: '4px 4px',
        background: 'var(--border)',
      }}
    />
  )
}
