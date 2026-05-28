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
