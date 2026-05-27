# link_set 内联标记重设计 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move link_set candidate selection from a full-candidate modal into inline ⬇ icons next to candidate `<a>` tags in the article body; convert 「📥 批量抓取」 into a confirm-only dialog scoped to inline-marked items.

**Architecture:** Frontend-only. Lift `markedURLs` state into `ArticlePage`; expose candidate/marked/fetched data + toggle callback to `MarkdownArticle` via a new `LinkSetContext` (must be context — `MarkdownArticle`'s `COMPONENTS` map is module-hoisted to keep `<ReactMarkdown>` AST stable and `<img>` from remounting). A new `<LinkSetMarkIcon>` button is rendered inline in the `a` override when the link's normalized URL hits the candidate set. The confirm dialog replaces `BatchFetchModal` and reads the same lifted state.

**Tech Stack:** React 18, TypeScript, react-markdown, no test framework (verify with `tsc --noEmit` + manual browser smoke per spec checklist).

**Spec reference:** `docs/superpowers/specs/2026-05-27-link-set-inline-marking-design.md`

**Branch:** `feature/link-set-inline-marking` (PR #33 already open).

---

## File Structure

| Path | Responsibility | Status |
|---|---|---|
| `frontend/src/utils/url.ts` | `normalizeURL(href, base)` — used by both context provider (candidate set) and `a` override (link href) | **Create** |
| `frontend/src/utils/linkSetSelection.ts` | `loadSavedURLs(id)` / `saveSelectedURLs(id, urls)` — extracted from current BatchFetchModal | **Create** |
| `frontend/src/components/LinkSetContext.tsx` | Context for sharing candidate set, mark state, toggle callback through ReactMarkdown without breaking module-scoped `COMPONENTS` | **Create** |
| `frontend/src/components/LinkSetMarkIcon.tsx` | Inline ⬇ / ✓ / ✅ button shown next to candidate links | **Create** |
| `frontend/src/components/BatchFetchConfirmDialog.tsx` | Confirm-only dialog: shows inline-marked rows, toolbar acts within displayed set, ✕ removes row | **Create** |
| `frontend/src/components/MarkdownArticle.tsx` | Add `useContext(LinkSetContext)` in `a` override; module-scoped `COMPONENTS` ref preserved | **Modify** |
| `frontend/src/pages/ArticlePage.tsx` | Own `candidates` + `markedURLs` state; localStorage sync; wrap `<MarkdownArticle>` with `<LinkSetContext.Provider>`; rewire 💡 button (no modal); swap `BatchFetchModal` → `BatchFetchConfirmDialog` | **Modify** |
| `frontend/src/components/BatchFetchModal.tsx` | Replaced entirely by `BatchFetchConfirmDialog` | **Delete** |

---

## Pre-flight

### Task 0: Branch + dependency check

**Files:** none

- [ ] **Step 0.1: Verify current branch**

Run: `git branch --show-current`
Expected: `feature/link-set-inline-marking`

- [ ] **Step 0.2: Verify spec is in tree**

Run: `ls docs/superpowers/specs/2026-05-27-link-set-inline-marking-design.md`
Expected: file exists

- [ ] **Step 0.3: Baseline type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS (no errors). This is the baseline — every later task must keep this clean.

---

## Task 1: URL normalization utility

**Files:**
- Create: `frontend/src/utils/url.ts`

- [ ] **Step 1.1: Create the file**

```ts
// frontend/src/utils/url.ts
// Resolves a possibly-relative href against the article's URL so it can be
// matched against absolute candidate URLs from the backend. Returns the
// original href if either input is unparseable (those will never match a
// candidate and fail safely).
export function normalizeURL(href: string, base?: string): string {
  try {
    return new URL(href, base).toString()
  } catch {
    return href
  }
}
```

- [ ] **Step 1.2: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 1.3: Commit**

```bash
git add frontend/src/utils/url.ts
git commit -m "feat(link-set): add normalizeURL utility"
```

---

## Task 2: localStorage selection helpers

**Files:**
- Create: `frontend/src/utils/linkSetSelection.ts`

- [ ] **Step 2.1: Create the file**

```ts
// frontend/src/utils/linkSetSelection.ts
// Per-article persistent selection set for the link_set inline marking flow.
// Extracted from the legacy BatchFetchModal so both the article page (writer)
// and the confirm dialog (reader, indirectly via the page) share one path.
//
// Why localStorage with TTL instead of server-side: selection is a per-device
// scratchpad — the user can mark candidates over several reading sessions
// then submit a batch. Cross-device sync isn't worth a new API surface for
// this. 1-day TTL keeps abandoned selections from accumulating forever.

const SELECTION_TTL_MS = 24 * 60 * 60 * 1000

const selectionKey = (articleId: number) => `rsspal_batch_sel_${articleId}`

export function loadSavedURLs(articleId: number): string[] {
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

export function saveSelectedURLs(articleId: number, urls: string[]): void {
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
```

- [ ] **Step 2.2: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 2.3: Commit**

```bash
git add frontend/src/utils/linkSetSelection.ts
git commit -m "feat(link-set): extract localStorage selection helpers"
```

---

## Task 3: LinkSetContext

**Files:**
- Create: `frontend/src/components/LinkSetContext.tsx`

**Why context (not props):** `MarkdownArticle.tsx` hoists its `COMPONENTS` map to module scope so that `<ReactMarkdown>` sees a stable reference and doesn't rebuild the AST + remount lazy `<img>` elements on every parent re-render (see the comment at lines 81–86 of `MarkdownArticle.tsx`). Threading new props into the `a` override would force us to either pass props that change every render (breaking that invariant) or build the override inside `useMemo` (same outcome — the reference changes when state changes). Context lets the `a` override subscribe to the changing values via `useContext` while `COMPONENTS` itself stays static.

- [ ] **Step 3.1: Create the file**

```tsx
// frontend/src/components/LinkSetContext.tsx
import { createContext } from 'react'

export type LinkSetContextValue = {
  candidateURLs: Set<string>        // normalized candidate URLs
  markedURLs: Set<string>            // subset user has clicked the icon for
  alreadyFetchedURLs: Set<string>    // candidates already turned into children
  normalize: (href: string) => string  // article-base-aware normalizer
  onToggleMark: (url: string) => void  // toggles a URL in markedURLs
}

// null means the article either isn't in link_set mode or the provider
// hasn't mounted yet — consumers must handle null and render the plain link.
export const LinkSetContext = createContext<LinkSetContextValue | null>(null)
```

- [ ] **Step 3.2: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 3.3: Commit**

```bash
git add frontend/src/components/LinkSetContext.tsx
git commit -m "feat(link-set): add LinkSetContext for icon state propagation"
```

---

## Task 4: LinkSetMarkIcon component

**Files:**
- Create: `frontend/src/components/LinkSetMarkIcon.tsx`

- [ ] **Step 4.1: Create the file**

```tsx
// frontend/src/components/LinkSetMarkIcon.tsx
// Small trailing icon rendered next to candidate <a> tags inside article
// markdown. Click toggles the "mark for batch fetch" state. Stops both
// default and propagation so the surrounding link doesn't navigate.

import type { MouseEvent } from 'react'

type Props = {
  marked: boolean
  alreadyFetched: boolean
  onToggle: () => void
}

export function LinkSetMarkIcon({ marked, alreadyFetched, onToggle }: Props) {
  const handleClick = (e: MouseEvent<HTMLButtonElement>) => {
    e.preventDefault()
    e.stopPropagation()
    if (alreadyFetched) return
    onToggle()
  }

  const title = alreadyFetched
    ? '已抓取'
    : marked
      ? '取消标记（不会抓取）'
      : '标记为批量抓取'

  // SVG icons inline so we don't need a sprite or icon library.
  // ⬇ outline for unmarked, ⬇ filled for marked, ✓-in-circle for fetched.
  const icon = alreadyFetched ? (
    <svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true">
      <circle cx="8" cy="8" r="7" fill="currentColor" opacity="0.2" />
      <path
        d="M4.5 8.5 L7 11 L11.5 5.5"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
    </svg>
  ) : marked ? (
    <svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true">
      <path
        d="M8 2 V11 M4 7 L8 11 L12 7 M3 13.5 H13"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
    </svg>
  ) : (
    <svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true">
      <path
        d="M8 2 V11 M4 7 L8 11 L12 7 M3 13.5 H13"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
        opacity="0.55"
      />
    </svg>
  )

  return (
    <button
      type="button"
      onClick={handleClick}
      title={title}
      aria-label={title}
      aria-pressed={marked}
      disabled={alreadyFetched}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        verticalAlign: 'baseline',
        marginLeft: 4,
        padding: 1,
        width: 16,
        height: 16,
        border: 'none',
        background: 'transparent',
        cursor: alreadyFetched ? 'not-allowed' : 'pointer',
        color: alreadyFetched
          ? 'var(--fg-muted)'
          : marked
            ? 'var(--accent, #2563eb)'
            : 'var(--fg-muted)',
        opacity: alreadyFetched ? 0.5 : marked ? 1 : 0.6,
        transition: 'opacity 120ms, color 120ms',
        lineHeight: 1,
      }}
      onMouseEnter={(e) => { if (!alreadyFetched && !marked) e.currentTarget.style.opacity = '1' }}
      onMouseLeave={(e) => { if (!alreadyFetched && !marked) e.currentTarget.style.opacity = '0.6' }}
    >
      {icon}
    </button>
  )
}
```

- [ ] **Step 4.2: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 4.3: Commit**

```bash
git add frontend/src/components/LinkSetMarkIcon.tsx
git commit -m "feat(link-set): add LinkSetMarkIcon trailing button"
```

---

## Task 5: Wire LinkSetContext into MarkdownArticle

**Files:**
- Modify: `frontend/src/components/MarkdownArticle.tsx`

- [ ] **Step 5.1: Update imports**

At the top of `frontend/src/components/MarkdownArticle.tsx`, change line 1:

```tsx
import { memo, useContext, useMemo, useState } from 'react'
```

(`useContext` is already imported in the current file — verify it is. If absent, add it.)

Then add these imports near the other component imports (around line 15):

```tsx
import { LinkSetContext } from './LinkSetContext'
import { LinkSetMarkIcon } from './LinkSetMarkIcon'
```

- [ ] **Step 5.2: Replace the `a` override**

In the same file, locate the existing `a` override inside `COMPONENTS` (lines 112–116):

```tsx
  a: ({ href, children, ...rest }) => (
    <a href={href} target="_blank" rel="noopener noreferrer" {...rest}>
      {children}
    </a>
  ),
```

Replace it with:

```tsx
  a: ({ href, children, ...rest }) => {
    const ctx = useContext(LinkSetContext)
    const normalized = href && ctx ? ctx.normalize(href) : null
    const isCandidate = normalized != null && ctx?.candidateURLs.has(normalized) === true
    const anchor = (
      <a href={href} target="_blank" rel="noopener noreferrer" {...rest}>
        {children}
      </a>
    )
    if (!isCandidate || !ctx || normalized == null) return anchor
    return (
      <span style={{ display: 'inline' }}>
        {anchor}
        <LinkSetMarkIcon
          marked={ctx.markedURLs.has(normalized)}
          alreadyFetched={ctx.alreadyFetchedURLs.has(normalized)}
          onToggle={() => ctx.onToggleMark(normalized)}
        />
      </span>
    )
  },
```

- [ ] **Step 5.3: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS. Note: `useContext` inside a function passed as a component prop is fine — React calls it as a hook because react-markdown renders it as a component, not as a function call.

- [ ] **Step 5.4: Commit**

```bash
git add frontend/src/components/MarkdownArticle.tsx
git commit -m "feat(link-set): consume LinkSetContext in markdown a override"
```

---

## Task 6: BatchFetchConfirmDialog component

**Files:**
- Create: `frontend/src/components/BatchFetchConfirmDialog.tsx`

The dialog needs to read `markedURLs` + the candidate list, render only marked candidates as rows, manage a separate `checkedURLs` for "actually fetch this round," support ✕-to-unmark (calls parent), 全选/反选/取消全选 within displayed rows, and submit to `batchFetchCandidates`.

- [ ] **Step 6.1: Create the file**

```tsx
// frontend/src/components/BatchFetchConfirmDialog.tsx
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
```

- [ ] **Step 6.2: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 6.3: Commit**

```bash
git add frontend/src/components/BatchFetchConfirmDialog.tsx
git commit -m "feat(link-set): add BatchFetchConfirmDialog (confirm-only flow)"
```

---

## Task 7: Wire ArticlePage to new flow

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`

This is the largest task: lift state, plumb the context, swap the dialog, fix the 💡 button. Do it as a single edit pass — the changes are interlocking.

- [ ] **Step 7.1: Read current state structure**

Run: `grep -n "batchModalOpen\|BatchFetchModal" frontend/src/pages/ArticlePage.tsx`

Note all the locations — you'll replace each.

- [ ] **Step 7.2: Update imports**

Open `frontend/src/pages/ArticlePage.tsx`. Replace the existing line:

```tsx
import { BatchFetchModal } from '../components/BatchFetchModal'
```

with:

```tsx
import { BatchFetchConfirmDialog } from '../components/BatchFetchConfirmDialog'
import { LinkSetContext, type LinkSetContextValue } from '../components/LinkSetContext'
import { normalizeURL } from '../utils/url'
import { loadSavedURLs, saveSelectedURLs } from '../utils/linkSetSelection'
```

Also confirm `useEffect`, `useMemo`, `useCallback`, `useState` are already imported from `react`. If `useCallback` is not in the import list yet, add it.

Also confirm `getArticleCandidates` and `CandidateView` are accessible. `getArticleCandidates` is exported from `../api/client`; add to the existing client import if not present. Same for `CandidateView` as a type import.

- [ ] **Step 7.3: Add state + helpers near other state**

Find the block of `useState` calls near the top of the `ArticlePage` component (in the same area as `setArticle`, `setLinkSetChildren`, `setBatchModalOpen`). After them, add:

```tsx
const [candidates, setCandidates] = useState<CandidateView[] | null>(null)
const [markedURLs, setMarkedURLs] = useState<Set<string>>(new Set())
const [confirmOpen, setConfirmOpen] = useState(false)
```

Then **delete** the existing `const [batchModalOpen, setBatchModalOpen] = useState(false)` line (replaced by `confirmOpen`).

- [ ] **Step 7.4: Build the URL normalizer + sets**

After the state declarations and once `article` is in scope, add:

```tsx
const articleURL = article?.url
const normalize = useCallback(
  (href: string) => normalizeURL(href, articleURL),
  [articleURL],
)

const candidateURLSet = useMemo(() => {
  const s = new Set<string>()
  for (const c of candidates ?? []) s.add(normalize(c.url))
  return s
}, [candidates, normalize])

const alreadyFetchedURLSet = useMemo(() => {
  const s = new Set<string>()
  for (const c of candidates ?? []) {
    if (c.already_fetched) s.add(normalize(c.url))
  }
  return s
}, [candidates, normalize])

const toggleMark = useCallback((url: string) => {
  setMarkedURLs((prev) => {
    const next = new Set(prev)
    if (next.has(url)) next.delete(url)
    else next.add(url)
    return next
  })
}, [])

const linkSetCtxValue = useMemo<LinkSetContextValue | null>(() => {
  if (!article?.links_extendable || !candidates) return null
  return {
    candidateURLs: candidateURLSet,
    markedURLs,
    alreadyFetchedURLs: alreadyFetchedURLSet,
    normalize,
    onToggleMark: toggleMark,
  }
}, [article?.links_extendable, candidates, candidateURLSet, markedURLs, alreadyFetchedURLSet, normalize, toggleMark])
```

- [ ] **Step 7.5: Add candidate-fetch + localStorage sync effects**

After the previous block, add:

```tsx
// Fetch candidate list whenever the article enters link_set mode.
useEffect(() => {
  if (!article?.id || !article?.links_extendable) {
    setCandidates(null)
    setMarkedURLs(new Set())
    return
  }
  let cancelled = false
  getArticleCandidates(article.id)
    .then((cands) => {
      if (cancelled) return
      setCandidates(cands)
      // Restore inline marks from localStorage. We normalize both sides so
      // a relative-href mark from a previous session still matches the
      // absolute candidate URL.
      const saved = loadSavedURLs(article.id)
      if (saved.length === 0) {
        setMarkedURLs(new Set())
        return
      }
      const candSet = new Set<string>()
      for (const c of cands) candSet.add(normalizeURL(c.url, article.url))
      const restored = new Set<string>()
      for (const u of saved) {
        const n = normalizeURL(u, article.url)
        if (candSet.has(n)) restored.add(n)
      }
      setMarkedURLs(restored)
    })
    .catch((e) => {
      console.warn('getArticleCandidates failed', e)
    })
  return () => { cancelled = true }
}, [article?.id, article?.links_extendable, article?.url])

// Persist marks to localStorage on every change.
useEffect(() => {
  if (!article?.id) return
  saveSelectedURLs(article.id, Array.from(markedURLs))
}, [markedURLs, article?.id])
```

- [ ] **Step 7.6: Wrap `<MarkdownArticle>` with the context provider**

Find the existing render of `<MarkdownArticle source={article.content} />` (around line 1218). Replace that single element with:

```tsx
<LinkSetContext.Provider value={linkSetCtxValue}>
  <MarkdownArticle source={article.content} />
</LinkSetContext.Provider>
```

`linkSetCtxValue` is `null` whenever the article is not in link_set mode, so the consumer in `MarkdownArticle` short-circuits to the plain link — no behavior change for non-link-set articles.

- [ ] **Step 7.7: Update the 「💡 转为 link_set」 button**

Find the button block (currently around lines 1277–1296). Replace its `onActivate` body — remove the `setBatchModalOpen(true)` line. The remaining body should re-fetch the article so `links_extendable=true` lands in state (which then triggers the candidate-fetch effect):

```tsx
onActivate={async () => {
  try {
    await confirmLinkSetSuggestion(article.id)
    const data = await getArticle(article.id)
    setArticle(data.article)
    setLinkSetChildren(data.children ?? null)
    // candidate fetch + inline icons kick in via the effect above
  } catch (e) {
    console.warn('confirm link_set failed', e)
    toast.error('转换失败，请稍后重试')
  }
}}
```

- [ ] **Step 7.8: Update the 「📥 批量抓取」 button**

Find the block (around line 1267). Change:

```tsx
onActivate={() => setBatchModalOpen(true)}
```

to:

```tsx
onActivate={() => setConfirmOpen(true)}
```

- [ ] **Step 7.9: Replace `<BatchFetchModal>` with `<BatchFetchConfirmDialog>`**

Find the existing block (around lines 1298–1312):

```tsx
<BatchFetchModal
  open={batchModalOpen}
  articleId={article.id}
  onClose={() => setBatchModalOpen(false)}
  onFetched={async (_n) => {
    try {
      const data = await getArticle(article.id)
      setArticle(data.article)
      setLinkSetChildren(data.children ?? null)
    } catch (e) {
      console.warn('refresh after batch_fetch failed', e)
    }
  }}
/>
```

Replace with:

```tsx
<BatchFetchConfirmDialog
  open={confirmOpen}
  articleId={article.id}
  candidates={candidates ?? []}
  markedURLs={markedURLs}
  normalize={normalize}
  onUnmark={(url) => toggleMark(url)}
  onClose={() => setConfirmOpen(false)}
  onFetched={async (_n) => {
    setMarkedURLs(new Set())
    // Re-fetch candidates so already_fetched flags refresh, then re-fetch
    // article for new children. Order matters: candidates first so the
    // marks-clear effect doesn't race with a stale candidate list.
    try {
      if (article?.id) {
        const cands = await getArticleCandidates(article.id)
        setCandidates(cands)
        const data = await getArticle(article.id)
        setArticle(data.article)
        setLinkSetChildren(data.children ?? null)
      }
    } catch (e) {
      console.warn('refresh after batch_fetch failed', e)
    }
  }}
/>
```

- [ ] **Step 7.10: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS. If there are errors:
- Missing `useCallback` import → add to react import line
- `CandidateView` not found → add to client import as `import { ..., type CandidateView } from '../api/client'`
- `getArticleCandidates` not imported → add it
- Any other type errors should be addressable with the exact fixes shown in earlier steps; do not invent new types

- [ ] **Step 7.11: Commit**

```bash
git add frontend/src/pages/ArticlePage.tsx
git commit -m "feat(link-set): lift state + wire LinkSetContext + swap to confirm dialog"
```

---

## Task 8: Delete BatchFetchModal

**Files:**
- Delete: `frontend/src/components/BatchFetchModal.tsx`

- [ ] **Step 8.1: Verify no remaining importers**

Run: `grep -rn "BatchFetchModal" frontend/src/`
Expected: zero matches (the only previous reference was in ArticlePage.tsx, already replaced in Task 7).

If anything still imports it, that's a Task 7 step you missed — go back and fix before deleting.

- [ ] **Step 8.2: Delete the file**

Run: `git rm frontend/src/components/BatchFetchModal.tsx`

- [ ] **Step 8.3: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 8.4: Commit**

```bash
git commit -m "refactor(link-set): remove obsolete BatchFetchModal"
```

---

## Task 9: Build + manual verification

**Files:** none modified — verification only.

- [ ] **Step 9.1: Production build**

Run: `cd frontend && npm run build`
Expected: PASS (vite outputs a `dist/` bundle without TS errors).

- [ ] **Step 9.2: Docker rebuild frontend**

Run from repo root: `docker-compose up -d --build frontend`
Expected: container restarts cleanly. Tail logs briefly if you want to confirm: `docker-compose logs --tail=20 frontend`.

Reminder: per the project's persisted convention, frontend Docker rebuild is required after every `frontend/src/` edit — nginx serves a pre-built bundle and there's no hot reload.

- [ ] **Step 9.3: Manual smoke test — happy path (Suggested article)**

Open `http://localhost/articles/<id>` for an RSS list-type article with `link_set_suggested=true` and `links_extendable !== true`. Verify:

1. ✅ Top of page shows the「💡 转为 link_set」 FAB (small outline).
2. Click it → no modal pops; FAB disappears; 「📥 批量抓取」 FAB appears; inline ⬇ icons appear next to candidate links in the article body.
3. Click an inline ⬇ icon → it fills (color: accent blue). The clicked link does **not** navigate.
4. Click the same icon again → it fades back to grey.
5. Mark 2–3 candidates → click 「📥 批量抓取」 → dialog opens listing only those rows, in original candidate order, with checkboxes pre-checked.
6. Click 取消全选 → all checkboxes clear, 「开始抓取」 button greys out, rows remain visible.
7. Click 全选 → all checkboxes re-check.
8. Click 反选 → checked rows uncheck and vice versa.
9. Click ✕ on a row → row disappears from dialog. Close dialog. Verify the corresponding inline icon in the article also went back to grey.
10. Re-open dialog. Click 「开始抓取」 → request completes, dialog closes, inline icons all clear, LinkSetChildren panel shows the new children (processing → ready over time).

- [ ] **Step 9.4: Manual smoke test — already-extendable article**

Open an article where `links_extendable=true` already (i.e. a previously-confirmed link_set article):

1. No 「💡」 button shown.
2. 「📥 批量抓取」 FAB is visible.
3. Inline ⬇ icons appear automatically next to candidate links.
4. Already-fetched candidates show ✅ grey icons that are not clickable.
5. The 「📥」 dialog excludes already-fetched candidates from the "可抓取" count.

- [ ] **Step 9.5: Manual smoke test — localStorage persistence**

1. Mark 2 candidates → reload page (no submit).
2. After reload, icons for those 2 candidates are still filled.
3. Open 「📥」 → both rows present.

- [ ] **Step 9.6: Manual smoke test — non-link-set article**

Open a regular article with `links_extendable !== true` and `link_set_suggested !== true`:

1. No 「💡」 or 「📥」 FAB.
2. No inline icons next to any links.
3. Links navigate normally on click.

- [ ] **Step 9.7: Manual smoke test — image-heavy article (regression check)**

Open any article with multiple images (an article like `/articles/2273`):

1. Images load progressively (no flicker / re-fetch storm on scroll).
2. This verifies that the LinkSetContext-driven changes did not break the `MarkdownArticle` AST stability invariant.

- [ ] **Step 9.8: Commit the build verification**

If everything passed, no code changes are needed — skip the commit. If any smoke test surfaced a bug, fix it as its own task (re-run tsc, commit, re-rebuild).

---

## Task 10: Push to PR

**Files:** none modified — push + summary.

- [ ] **Step 10.1: Push to remote**

Run: `git push origin feature/link-set-inline-marking`
Expected: PR #33 updates with all new commits.

- [ ] **Step 10.2: Post a summary comment**

Run:

```bash
gh pr comment 33 --body "$(cat <<'EOF'
Implementation complete. Summary of commits:

- `feat(link-set): add normalizeURL utility`
- `feat(link-set): extract localStorage selection helpers`
- `feat(link-set): add LinkSetContext for icon state propagation`
- `feat(link-set): add LinkSetMarkIcon trailing button`
- `feat(link-set): consume LinkSetContext in markdown a override`
- `feat(link-set): add BatchFetchConfirmDialog (confirm-only flow)`
- `feat(link-set): lift state + wire LinkSetContext + swap to confirm dialog`
- `refactor(link-set): remove obsolete BatchFetchModal`

Manual smoke tests from the plan checklist all passed. Ready for review.
EOF
)"
```

- [ ] **Step 10.3: Confirm PR diff is coherent**

Run: `gh pr diff 33 --name-only | sort`
Expected: should list exactly:
```
docs/superpowers/specs/2026-05-27-link-set-inline-marking-design.md
frontend/src/components/BatchFetchConfirmDialog.tsx
frontend/src/components/BatchFetchModal.tsx
frontend/src/components/LinkSetContext.tsx
frontend/src/components/LinkSetMarkIcon.tsx
frontend/src/components/MarkdownArticle.tsx
frontend/src/pages/ArticlePage.tsx
frontend/src/utils/linkSetSelection.ts
frontend/src/utils/url.ts
```

(`BatchFetchModal.tsx` appears because it was deleted — `--name-only` shows deletions too.)

---

## Self-Review

**Spec coverage:**
- "状态门不变 — 仅 `links_extendable=true` 时显示图标 / fab" → Task 7.4 (`linkSetCtxValue` returns null when `!article?.links_extendable`) + Task 7.6 (provider wrap)
- "持久化沿用 localStorage" → Task 2 (extracted helpers) + Task 7.5 (effect)
- "URL 归一化" → Task 1 + Task 7.4 + spec edge case "多次出现的同 URL 共享状态" (single Set keyed by normalized URL)
- "已抓取 ✅ 灰" → Task 4 (visual) + Task 5 (renders only when alreadyFetched) + Task 6 (dialog row disabled)
- "工具栏作用范围 = 显示行" → Task 6 selectAll/deselectAll/invertSelection only iterate `rows`
- "对话框不能扩张" → Task 6 `rows = candidates.filter(c => markedURLs.has(...))` is the sole source
- "💡 不再开 Modal" → Task 7.7
- "成功后清空 marks" → Task 7.9 (`setMarkedURLs(new Set())` in `onFetched`)
- "删除 BatchFetchModal" → Task 8

**Placeholder scan:** none. All code blocks are complete; "exact commands with expected output" is satisfied per task.

**Type consistency:** `normalize` signature is `(href: string) => string` everywhere; `markedURLs` is `Set<string>` everywhere; `LinkSetContextValue` shape matches consumer expectations in Task 5; `BatchFetchConfirmDialog` `onUnmark: (url: string) => void` matches Task 7.9's `(url) => toggleMark(url)`.

**Scope:** single focused frontend feature, no DB / backend / worker changes.
