import { useEffect, useMemo, useState } from 'react'
import { CandidateView, batchFetchCandidates, getArticleCandidates } from '../api/client'

interface Props {
  open: boolean
  articleId: number
  onClose: () => void
  onFetched?: (insertedCount: number) => void
}

const SELECTION_TTL_MS = 24 * 60 * 60 * 1000 // 1 day
const selectionKey = (id: number) => `rsspal_batch_sel_${id}`

function loadSavedURLs(articleId: number): string[] {
  try {
    const raw = localStorage.getItem(selectionKey(articleId))
    if (!raw) return []
    const parsed = JSON.parse(raw) as { urls?: unknown; savedAt?: unknown }
    if (typeof parsed?.savedAt !== 'number') return []
    if (Date.now() - parsed.savedAt > SELECTION_TTL_MS) {
      localStorage.removeItem(selectionKey(articleId))
      return []
    }
    if (!Array.isArray(parsed.urls)) return []
    return parsed.urls.filter((u): u is string => typeof u === 'string')
  } catch {
    return []
  }
}

function saveSelectedURLs(articleId: number, urls: string[]) {
  try {
    if (urls.length === 0) {
      localStorage.removeItem(selectionKey(articleId))
      return
    }
    localStorage.setItem(
      selectionKey(articleId),
      JSON.stringify({ urls, savedAt: Date.now() }),
    )
  } catch {
    /* quota or disabled — ignore */
  }
}

export function BatchFetchModal({ open, articleId, onClose, onFetched }: Props) {
  const [candidates, setCandidates] = useState<CandidateView[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [lastClickedIdx, setLastClickedIdx] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setLoading(true)
    setError(null)
    setSelected(new Set())
    setLastClickedIdx(null)
    getArticleCandidates(articleId)
      .then((cands) => {
        setCandidates(cands)
        // Restore prior selection (if within 1-day TTL); map URLs back to indices
        // and drop URLs that no longer appear or are already-fetched.
        const savedURLs = loadSavedURLs(articleId)
        if (savedURLs.length === 0) return
        const savedSet = new Set(savedURLs)
        const restored = new Set<number>()
        cands.forEach((c, i) => {
          if (!c.already_fetched && savedSet.has(c.url)) restored.add(i)
        })
        if (restored.size > 0) setSelected(restored)
      })
      .catch((e) => setError(e?.response?.data?.error || '获取候选链接失败'))
      .finally(() => setLoading(false))
  }, [open, articleId])

  // Persist selection on every change (debounce isn't worth it — localStorage is fast).
  useEffect(() => {
    if (!candidates || !open) return
    const urls: string[] = []
    selected.forEach((i) => {
      const c = candidates[i]
      if (c && !c.already_fetched) urls.push(c.url)
    })
    saveSelectedURLs(articleId, urls)
  }, [selected, candidates, open, articleId])

  // Lock body scroll while modal is open
  useEffect(() => {
    if (!open) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = prev
      window.removeEventListener('keydown', onKey)
    }
  }, [open, onClose])

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
    } else {
      toggleIdx(idx)
    }
  }

  function selectAll() { setSelected(new Set(selectableIndices)) }
  function deselectAll() { setSelected(new Set()) }
  function invertSelection() {
    setSelected((prev) => {
      const next = new Set<number>()
      for (const i of selectableIndices) if (!prev.has(i)) next.add(i)
      return next
    })
  }

  async function handleConfirm() {
    if (!candidates || selected.size === 0) return
    setSubmitting(true)
    setError(null)
    try {
      const chosen = Array.from(selected).sort((a, b) => a - b).map((i) => ({
        title: candidates[i].title,
        url: candidates[i].url,
        editor_note: candidates[i].editor_note,
      }))
      const result = await batchFetchCandidates(articleId, chosen)
      // Successfully queued — clear saved selection so next open starts fresh.
      saveSelectedURLs(articleId, [])
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

  const totalSelectable = candidates?.filter((c) => !c.already_fetched).length ?? 0
  const confirmDisabled = submitting || selected.size === 0

  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0, 0, 0, 0.45)',
        zIndex: 2000,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 16,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          background: 'var(--bg)',
          border: '1px solid var(--border)',
          borderRadius: 8,
          width: '100%',
          maxWidth: 720,
          maxHeight: '85vh',
          display: 'flex',
          flexDirection: 'column',
          boxShadow: '0 8px 32px rgba(0, 0, 0, 0.25)',
        }}
      >
        {/* Header */}
        <div style={{
          padding: '14px 20px',
          borderBottom: '1px solid var(--border)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}>
          <h3 style={{ fontSize: 16, fontWeight: 600, margin: 0 }}>选择要抓取的链接</h3>
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭"
            style={{
              border: 'none',
              background: 'transparent',
              color: 'var(--fg-muted)',
              fontSize: 22,
              lineHeight: 1,
              cursor: 'pointer',
              padding: 0,
            }}
          >×</button>
        </div>

        {/* Toolbar */}
        <div style={{
          padding: '10px 20px',
          borderBottom: '1px solid var(--border)',
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          flexWrap: 'wrap',
          fontSize: 12,
        }}>
          <button type="button" className="btn-ghost btn-sm" onClick={selectAll}>全选</button>
          <button type="button" className="btn-ghost btn-sm" onClick={invertSelection}>反选</button>
          <button type="button" className="btn-ghost btn-sm" onClick={deselectAll}>取消全选</button>
          <span style={{ marginLeft: 'auto', color: 'var(--fg-muted)' }}>
            已选 {selected.size} / 共 {totalSelectable} 可选
          </span>
        </div>

        {/* Body */}
        <div style={{ flex: 1, overflowY: 'auto', overflowX: 'hidden', padding: '8px 12px', minWidth: 0 }}>
          {loading && (
            <div style={{ padding: 32, textAlign: 'center', fontSize: 13, color: 'var(--fg-muted)' }}>加载候选中…</div>
          )}
          {error && !loading && (
            <div style={{ padding: 16, fontSize: 13, color: 'crimson' }}>{error}</div>
          )}
          {!loading && !error && candidates && candidates.length === 0 && (
            <div style={{ padding: 32, textAlign: 'center', fontSize: 13, color: 'var(--fg-muted)' }}>未找到可抓取的链接</div>
          )}
          {!loading && !error && candidates && candidates.length > 0 && (
            <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
              {candidates.map((c, i) => {
                const disabled = c.already_fetched
                const checked = selected.has(i)
                return (
                  <li
                    key={i}
                    onClick={(e) => !disabled && handleRowClick(e, i)}
                    style={{
                      display: 'grid',
                      gridTemplateColumns: '20px minmax(0, 1fr) auto',
                      alignItems: 'start',
                      columnGap: 10,
                      padding: '8px 10px',
                      marginBottom: 2,
                      borderRadius: 4,
                      fontSize: 13,
                      color: 'var(--fg)',
                      cursor: disabled ? 'not-allowed' : 'pointer',
                      opacity: disabled ? 0.5 : 1,
                      background: checked ? 'var(--surface-hover)' : 'transparent',
                      userSelect: 'none',
                      boxSizing: 'border-box',
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      disabled={disabled}
                      readOnly
                      onChange={() => {}}
                      onClick={(e) => e.stopPropagation()}
                      style={{ marginTop: 3 }}
                    />
                    <div style={{ minWidth: 0, overflow: 'hidden' }}>
                      <div style={{
                        fontWeight: 500,
                        color: 'var(--fg)',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}>
                        {c.title || '(无标题)'}
                      </div>
                      <div style={{
                        fontSize: 11,
                        color: 'var(--fg-muted)',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}>
                        {hostOf(c.url)}
                      </div>
                      {c.editor_note && (
                        <div style={{
                          fontSize: 11,
                          marginTop: 4,
                          fontStyle: 'italic',
                          color: 'var(--fg-muted)',
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}>
                          {c.editor_note}
                        </div>
                      )}
                    </div>
                    <div>
                      {disabled && (
                        <span style={{
                          fontSize: 11,
                          padding: '2px 8px',
                          borderRadius: 4,
                          background: 'var(--surface-hover)',
                          color: 'var(--fg-muted)',
                          whiteSpace: 'nowrap',
                        }}>已抓取</span>
                      )}
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </div>

        {/* Footer */}
        <div style={{
          padding: '12px 20px',
          borderTop: '1px solid var(--border)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-end',
          gap: 8,
        }}>
          <button
            type="button"
            onClick={onClose}
            style={{
              padding: '6px 14px',
              fontSize: 13,
              borderRadius: 4,
              border: '1px solid var(--border)',
              background: 'transparent',
              color: 'var(--fg)',
              cursor: 'pointer',
            }}
          >取消</button>
          <button
            type="button"
            onClick={handleConfirm}
            disabled={confirmDisabled}
            style={{
              padding: '6px 14px',
              fontSize: 13,
              borderRadius: 4,
              border: 'none',
              background: confirmDisabled ? 'var(--surface-hover)' : 'var(--accent)',
              color: confirmDisabled ? 'var(--fg-muted)' : 'var(--accent-fg)',
              cursor: confirmDisabled ? 'not-allowed' : 'pointer',
            }}
          >
            {submitting ? '抓取中…' : `抓取选中（${selected.size}）`}
          </button>
        </div>
      </div>
    </div>
  )
}
