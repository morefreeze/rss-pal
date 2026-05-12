import { useEffect, useMemo, useState } from 'react'
import { CandidateView, batchFetchCandidates, getArticleCandidates } from '../api/client'

interface Props {
  open: boolean
  articleId: number
  onClose: () => void
  onFetched?: (insertedCount: number) => void
}

export function BatchFetchModal({ open, articleId, onClose, onFetched }: Props) {
  const [candidates, setCandidates] = useState<CandidateView[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [lastClickedIdx, setLastClickedIdx] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Load candidates on open
  useEffect(() => {
    if (!open) return
    setLoading(true)
    setError(null)
    setSelected(new Set())
    setLastClickedIdx(null)
    getArticleCandidates(articleId)
      .then((cands) => setCandidates(cands))
      .catch((e) => setError(e?.response?.data?.error || '获取候选链接失败'))
      .finally(() => setLoading(false))
  }, [open, articleId])

  const selectableIndices = useMemo(() => {
    if (!candidates) return [] as number[]
    return candidates.map((c, i) => (c.already_fetched ? -1 : i)).filter((i) => i >= 0)
  }, [candidates])

  if (!open) return null

  function toggleIdx(idx: number) {
    if (!candidates) return
    if (candidates[idx].already_fetched) return
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(idx)) next.delete(idx)
      else next.add(idx)
      return next
    })
    setLastClickedIdx(idx)
  }

  function rangeSelect(toIdx: number) {
    if (!candidates || lastClickedIdx === null) {
      toggleIdx(toIdx)
      return
    }
    const lo = Math.min(lastClickedIdx, toIdx)
    const hi = Math.max(lastClickedIdx, toIdx)
    setSelected((prev) => {
      const next = new Set(prev)
      for (let i = lo; i <= hi; i++) {
        if (!candidates[i].already_fetched) next.add(i)
      }
      return next
    })
    setLastClickedIdx(toIdx)
  }

  function handleRowClick(e: React.MouseEvent, idx: number) {
    if (e.shiftKey) {
      e.preventDefault()
      rangeSelect(idx)
    } else if (e.ctrlKey || e.metaKey) {
      toggleIdx(idx)
    } else {
      toggleIdx(idx)
    }
  }

  function selectAll() {
    setSelected(new Set(selectableIndices))
  }
  function deselectAll() {
    setSelected(new Set())
  }
  function invertSelection() {
    setSelected((prev) => {
      const next = new Set<number>()
      for (const i of selectableIndices) {
        if (!prev.has(i)) next.add(i)
      }
      return next
    })
  }

  async function handleConfirm() {
    if (!candidates || selected.size === 0) return
    setSubmitting(true)
    setError(null)
    try {
      const chosen = Array.from(selected)
        .sort((a, b) => a - b)
        .map((i) => ({
          title: candidates[i].title,
          url: candidates[i].url,
          editor_note: candidates[i].editor_note,
        }))
      const result = await batchFetchCandidates(articleId, chosen)
      onFetched?.(result.inserted)
      onClose()
    } catch (e: any) {
      setError(e?.response?.data?.error || '抓取失败')
    } finally {
      setSubmitting(false)
    }
  }

  function hostOf(url: string): string {
    try { return new URL(url).host } catch { return url }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.4)' }}
      onClick={onClose}
    >
      <div
        className="rounded-lg w-full max-w-3xl mx-4 max-h-[80vh] flex flex-col"
        style={{ background: 'var(--bg)', border: '1px solid var(--border)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="px-5 py-3 flex items-center justify-between" style={{ borderBottom: '1px solid var(--border)' }}>
          <h3 className="text-base font-semibold">选择要抓取的链接</h3>
          <button type="button" className="text-xl leading-none" onClick={onClose} style={{ color: 'var(--fg-muted)' }}>×</button>
        </div>

        <div className="px-5 py-2 flex items-center gap-2 text-xs flex-wrap" style={{ borderBottom: '1px solid var(--border)' }}>
          <button type="button" onClick={selectAll} className="px-2 py-1 rounded" style={{ border: '1px solid var(--border)' }}>全选</button>
          <button type="button" onClick={invertSelection} className="px-2 py-1 rounded" style={{ border: '1px solid var(--border)' }}>反选</button>
          <button type="button" onClick={deselectAll} className="px-2 py-1 rounded" style={{ border: '1px solid var(--border)' }}>取消全选</button>
          <span className="ml-auto" style={{ color: 'var(--fg-muted)' }}>
            已选 {selected.size} / 共 {candidates?.filter((c) => !c.already_fetched).length ?? 0} 可选
          </span>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-2">
          {loading && <div className="py-8 text-center text-sm" style={{ color: 'var(--fg-muted)' }}>加载候选中…</div>}
          {error && !loading && <div className="py-4 text-sm" style={{ color: 'crimson' }}>{error}</div>}
          {!loading && !error && candidates && candidates.length === 0 && (
            <div className="py-8 text-center text-sm" style={{ color: 'var(--fg-muted)' }}>未找到可抓取的链接</div>
          )}
          {!loading && !error && candidates && candidates.length > 0 && (
            <ul className="space-y-1">
              {candidates.map((c, i) => {
                const disabled = c.already_fetched
                const checked = selected.has(i)
                return (
                  <li
                    key={i}
                    onClick={(e) => !disabled && handleRowClick(e, i)}
                    className={'flex items-start gap-3 p-2 rounded text-sm cursor-pointer ' + (disabled ? 'opacity-50 cursor-not-allowed' : '')}
                    style={{
                      background: checked ? 'var(--bg-elevated)' : 'transparent',
                      userSelect: 'none',
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      disabled={disabled}
                      onChange={() => {}}
                      onClick={(e) => e.stopPropagation()}
                      readOnly
                      style={{ marginTop: 3 }}
                    />
                    <div className="flex-1 min-w-0">
                      <div className="font-medium truncate">{c.title}</div>
                      <div className="text-xs truncate" style={{ color: 'var(--fg-muted)' }}>{hostOf(c.url)}</div>
                      {c.editor_note && (
                        <div className="text-xs mt-1 italic" style={{ color: 'var(--fg-muted)' }}>{c.editor_note}</div>
                      )}
                    </div>
                    {disabled && (
                      <span className="text-xs px-2 py-0.5 rounded" style={{ background: 'var(--bg-elevated)', color: 'var(--fg-muted)' }}>已抓取</span>
                    )}
                  </li>
                )
              })}
            </ul>
          )}
        </div>

        <div className="px-5 py-3 flex items-center justify-end gap-2" style={{ borderTop: '1px solid var(--border)' }}>
          <button type="button" onClick={onClose} className="px-3 py-1.5 text-sm rounded" style={{ border: '1px solid var(--border)' }}>
            取消
          </button>
          <button
            type="button"
            onClick={handleConfirm}
            disabled={submitting || selected.size === 0}
            className="px-3 py-1.5 text-sm rounded"
            style={{
              background: selected.size === 0 || submitting ? 'var(--bg-elevated)' : 'var(--accent)',
              color: selected.size === 0 || submitting ? 'var(--fg-muted)' : 'var(--accent-fg, white)',
              cursor: selected.size === 0 || submitting ? 'not-allowed' : 'pointer',
            }}
          >
            {submitting ? '抓取中…' : `抓取选中（${selected.size}）`}
          </button>
        </div>
      </div>
    </div>
  )
}
