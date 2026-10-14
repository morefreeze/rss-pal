import { useRef, useState } from 'react'
import { updateArticleNote } from '../api/client'
import './ArticleNote.css'

interface Props {
  articleId: number
  note: string
  onSaved: (note: string) => void
}

export default function ArticleNote({ articleId, note, onSaved }: Props) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(note)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const pending = useRef(false)
  const length = Array.from(draft).length
  const begin = () => { setDraft(note); setError(''); setEditing(true) }
  const save = async () => {
    if (pending.current || length > 10000) return
    pending.current = true
    setSaving(true)
    setError('')
    try {
      const result = await updateArticleNote(articleId, draft)
      onSaved(result.capture_note)
      setEditing(false)
    } catch (err) {
      const response = (err as { response?: { data?: { error?: string } } }).response
      setError(response?.data?.error || '保存失败，文字还在，可以再试一次。')
    } finally {
      pending.current = false
      setSaving(false)
    }
  }
  if (!note && !editing) {
    return <button className="article-note-add secondary" type="button" onClick={begin}>记下想法</button>
  }
  return (
    <section className="article-note" aria-label="我的随手记">
      <div className="article-note-heading">
        <label htmlFor={`article-note-${articleId}`}>我的随手记</label>
        <span className="text-muted text-sm">仅自己可见</span>
        {!editing && <button className="secondary" type="button" onClick={begin} aria-label="编辑随手记">编辑</button>}
      </div>
      {editing ? <>
        <p className="text-muted text-sm">为什么想保存、在哪里看到的，或想到怎么用？一句话也好。</p>
        <textarea id={`article-note-${articleId}`} autoFocus rows={4} value={draft} disabled={saving}
          placeholder="给未来的自己留一句话……"
          onChange={event => setDraft(event.target.value)}
          onKeyDown={event => {
            if (event.key === 'Enter' && (event.metaKey || event.ctrlKey) && !event.nativeEvent.isComposing) {
              event.preventDefault()
              void save()
            }
          }} />
        <div className="article-note-actions">
          <button type="button" disabled={saving || length > 10000} onClick={() => void save()} aria-label="保存随手记">{saving ? '保存中…' : '保存'}</button>
          <button type="button" className="secondary" disabled={saving} onClick={() => setEditing(false)}>取消</button>
          <span className="text-muted text-sm">{length} / 10000 · ⌘ / Ctrl ↵ 保存</span>
        </div>
        {error && <p role="alert">{error}</p>}
      </> : <p className="article-note-body">{note}</p>}
    </section>
  )
}
