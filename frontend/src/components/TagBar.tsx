import { useEffect, useRef, useState } from 'react'
import {
  ArticleTagsResponse,
  UserTag,
  addArticleTag,
  getArticleTags,
  listTags,
  removeArticleTag,
} from '../api/client'
import TagChip from './TagChip'

interface Props {
  articleId: number
}

export default function TagBar({ articleId }: Props) {
  const [data, setData] = useState<ArticleTagsResponse | null>(null)
  const [allTags, setAllTags] = useState<UserTag[]>([])
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    getArticleTags(articleId).then(setData).catch(() => setData(null))
    listTags().then(setAllTags).catch(() => setAllTags([]))
  }, [articleId])

  useEffect(() => {
    if (editing) inputRef.current?.focus()
  }, [editing])

  if (!data) return null

  const manualNames = new Set(data.manual.map(t => t.name))
  const suggestions = allTags
    .filter(t => t.name.toLowerCase().includes(draft.trim().toLowerCase()))
    .filter(t => !manualNames.has(t.name))
    .slice(0, 8)

  const submit = async (raw?: string) => {
    const name = (raw ?? draft).trim()
    if (!name) return
    await addArticleTag(articleId, name)
    setDraft('')
    setEditing(false)
    const fresh = await getArticleTags(articleId)
    setData(fresh)
    listTags().then(setAllTags)
  }

  const removeManual = async (tagId: number) => {
    await removeArticleTag(articleId, tagId)
    setData(d => (d ? { ...d, manual: d.manual.filter(t => t.id !== tagId) } : d))
  }

  return (
    <div className="flex flex-wrap items-center gap-2 my-3">
      <TagChip name={data.source.title} variant="source" />
      {data.manual.map(t => (
        <TagChip
          key={t.id}
          name={t.name}
          variant="manual"
          onRemove={() => removeManual(t.id)}
        />
      ))}
      {!editing ? (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="px-2 py-0.5 rounded-full text-xs border border-slate-300 text-slate-500 hover:bg-slate-50"
        >
          + 添加
        </button>
      ) : (
        <div className="relative">
          <input
            ref={inputRef}
            value={draft}
            onChange={e => setDraft(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter') submit()
              if (e.key === 'Escape') { setEditing(false); setDraft('') }
            }}
            onBlur={() => setTimeout(() => setEditing(false), 150)}
            placeholder="输入新建或选择已有"
            maxLength={64}
            className="px-2 py-0.5 rounded-full text-xs border border-slate-300 focus:outline-none focus:ring-1 focus:ring-sky-400"
          />
          {suggestions.length > 0 && (
            <div className="absolute top-full left-0 mt-1 z-10 bg-white border border-slate-200 rounded shadow-md text-xs">
              {suggestions.map(s => (
                <button
                  key={s.id}
                  type="button"
                  onMouseDown={e => { e.preventDefault(); submit(s.name) }}
                  className="block w-full text-left px-3 py-1 hover:bg-slate-100"
                >
                  {s.name}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
