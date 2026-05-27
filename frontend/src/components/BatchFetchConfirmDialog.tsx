// Confirm-only dialog for the link_set inline marking flow.
//
// Selection happens in the article body via <LinkSetMarkIcon>; this dialog
// shows what the user already chose and lets them: (1) uncheck rows for
// this round without giving them up entirely, (2) ✕ to remove a candidate
// from the inline marks, (3) confirm and dispatch a batch_fetch.
//
// Toolbar 全选/反选/取消全选 operate ONLY on the displayed rows — the dialog
// cannot expand beyond what the user marked in-page. To add more, close
// and click more icons.

import { useEffect, useMemo, useState } from 'react'
import {
  CandidateView,
  batchFetchCandidates,
} from '../api/client'

interface Props {
  open: boolean
  articleId: number
  candidates: CandidateView[]               // full candidate list (already fetched from API)
  markedURLs: Set<string>                    // normalized URLs the user marked in-page
  normalize: (href: string) => string        // same normalizer used by the icon context
  onUnmark: (normalizedURL: string) => void  // ✕ → parent removes from markedURLs
  onClose: () => void
  onFetched?: (insertedCount: number) => void
}

export function BatchFetchConfirmDialog({
  open,
  articleId,
  candidates,
  markedURLs,
  normalize,
  onUnmark,
  onClose,
  onFetched,
}: Props) {
  // Filter + order: keep candidates' original order (= position from API)
  const rows = useMemo(
    () => candidates.filter((c) => markedURLs.has(normalize(c.url))),
    [candidates, markedURLs, normalize],
  )

  // Per-row "include in this fetch" state. Identified by normalized URL so
  // it survives rows shifting in/out as the user ✕s things.
  const [checkedURLs, setCheckedURLs] = useState<Set<string>>(new Set())
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // On open: default-check every selectable row (not already_fetched).
  useEffect(() => {
    if (!open) return
    setError(null)
    setSubmitting(false)
    const initial = new Set<string>()
    for (const c of rows) {
      if (!c.already_fetched) initial.add(normalize(c.url))
    }
    setCheckedURLs(initial)
    // We intentionally do not depend on `rows` here — that would reset
    // the user's per-row checkbox edits every time markedURLs/candidates
    // shift. Only re-init when the dialog re-opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  // Drop checks for URLs the user ✕'d away while the dialog is open.
  useEffect(() => {
    if (!open) return
    setCheckedURLs((prev) => {
      const validURLs = new Set(rows.map((c) => normalize(c.url)))
      let changed = false
      const next = new Set<string>()
      for (const u of prev) {
        if (validURLs.has(u)) next.add(u)
        else changed = true
      }
      return changed ? next : prev
    })
  }, [rows, open, normalize])

  // Lock body scroll while open + ESC to close
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

  if (!open) return null

  function toggleChecked(url: string) {
    setCheckedURLs((prev) => {
      const next = new Set(prev)
      if (next.has(url)) next.delete(url)
      else next.add(url)
      return next
    })
  }

  function selectAll() {
    const all = new Set<string>()
    for (const c of rows) {
      if (!c.already_fetched) all.add(normalize(c.url))
    }
    setCheckedURLs(all)
  }
  function deselectAll() {
    setCheckedURLs(new Set())
  }
  function invertSelection() {
    setCheckedURLs((prev) => {
      const next = new Set<string>()
      for (const c of rows) {
        if (c.already_fetched) continue
        const url = normalize(c.url)
        if (!prev.has(url)) next.add(url)
      }
      return next
    })
  }

  async function handleConfirm() {
    if (checkedURLs.size === 0) return
    setSubmitting(true)
    setError(null)
    try {
      // Submit in display order to preserve user intent.
      const chosen = rows
        .filter((c) => !c.already_fetched && checkedURLs.has(normalize(c.url)))
        .map((c) => ({
          title: c.title,
          url: c.url,
          editor_note: c.editor_note,
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

  const totalSelectable = rows.filter((c) => !c.already_fetched).length
  const confirmDisabled = submitting || checkedURLs.size === 0

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
          <h3 style={{ fontSize: 16, fontWeight: 600, margin: 0 }}>确认要抓取的链接</h3>
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
            已勾选 {checkedURLs.size} / 共 {totalSelectable} 可抓取
          </span>
        </div>

        {/* Body */}
        <div style={{ flex: 1, overflowY: 'auto', overflowX: 'hidden', padding: '8px 12px', minWidth: 0 }}>
          {error && (
            <div style={{ padding: 16, fontSize: 13, color: 'crimson' }}>{error}</div>
          )}
          {rows.length === 0 && !error && (
            <div style={{ padding: 32, textAlign: 'center', fontSize: 13, color: 'var(--fg-muted)' }}>
              还没有标记任何链接。回到正文，点击候选链接旁的小图标进行标记。
            </div>
          )}
          {rows.length > 0 && (
            <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
              {rows.map((c) => {
                const url = normalize(c.url)
                const disabled = c.already_fetched
                const checked = checkedURLs.has(url)
                return (
                  <li
                    key={url}
                    style={{
                      display: 'grid',
                      gridTemplateColumns: '20px minmax(0, 1fr) auto auto',
                      alignItems: 'start',
                      columnGap: 10,
                      padding: '8px 10px',
                      marginBottom: 2,
                      borderRadius: 4,
                      fontSize: 13,
                      color: 'var(--fg)',
                      opacity: disabled ? 0.5 : 1,
                      background: checked ? 'var(--surface-hover)' : 'transparent',
                      boxSizing: 'border-box',
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      disabled={disabled}
                      onChange={() => !disabled && toggleChecked(url)}
                      style={{ marginTop: 3, cursor: disabled ? 'not-allowed' : 'pointer' }}
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
                    <button
                      type="button"
                      onClick={() => onUnmark(url)}
                      title="从标记中移除"
                      aria-label="从标记中移除"
                      style={{
                        border: 'none',
                        background: 'transparent',
                        color: 'var(--fg-muted)',
                        fontSize: 16,
                        lineHeight: 1,
                        padding: '0 4px',
                        cursor: 'pointer',
                      }}
                    >×</button>
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
            {submitting ? '抓取中…' : `开始抓取（${checkedURLs.size}）`}
          </button>
        </div>
      </div>
    </div>
  )
}
