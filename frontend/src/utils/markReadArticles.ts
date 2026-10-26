import { invalidateArticleDetail } from '../api/articleDetailCache'

export function recordReadArticles(ids: number[]) {
  for (const id of ids) invalidateArticleDetail(id)
  try {
    const stored: number[] = JSON.parse(
      sessionStorage.getItem('readArticles') || '[]',
    )
    sessionStorage.setItem(
      'readArticles',
      JSON.stringify([...new Set([...stored, ...ids])]),
    )
  } catch {
    /* Storage can be unavailable in private browsing. */
  }
  window.dispatchEvent(new Event('refresh-unread'))
}
