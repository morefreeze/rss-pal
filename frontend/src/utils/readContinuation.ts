interface ReadRow { id: number; is_read?: boolean }

export interface ReadContinuation {
  anchorId: number
  nextUnreadId?: number
  unreadOnly: boolean
  readIds: ReadonlySet<number>
}

// Remember the article itself, not its index: marking above can remove rows
// from unread-only lists, and a refresh can insert newly fetched articles.
export function captureReadContinuation(
  rows: ReadRow[], anchorId: number, unreadOnly: boolean,
  readIds: ReadonlySet<number> = new Set(),
): ReadContinuation {
  const anchorIndex = rows.findIndex(row => row.id === anchorId)
  const next = anchorIndex < 0 ? undefined : rows.slice(anchorIndex + 1)
    .find(row => !row.is_read && !readIds.has(row.id))
  return { anchorId, nextUnreadId: next?.id, unreadOnly, readIds: new Set(readIds) }
}

export function findReadContinuation(
  rows: ReadRow[], target: ReadContinuation, exhausted = false,
): number | undefined {
  let start = 0
  if (target.nextUnreadId !== undefined) {
    start = rows.findIndex(row => row.id === target.nextUnreadId)
    // An unread target may disappear because it was read elsewhere. Only an
    // exhausted unread-only result has a safe fallback to its first row.
    if (start < 0 && !(exhausted && target.unreadOnly)) return undefined
    start = Math.max(0, start)
  } else if (!target.unreadOnly) {
    const anchorIndex = rows.findIndex(row => row.id === target.anchorId)
    if (anchorIndex < 0) return undefined
    start = anchorIndex + 1
  }
  return rows.slice(start).find(row => !row.is_read && !target.readIds.has(row.id))?.id
}

export function scrollToReadContinuation(id: number) {
  document.querySelector<HTMLElement>(`[data-article-id="${id}"]`)
    ?.scrollIntoView({ block: 'start', behavior: 'instant' })
}
