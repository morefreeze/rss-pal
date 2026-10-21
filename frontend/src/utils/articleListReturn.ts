import type { ArticleListItem } from '../api/client'
const KEY = 'articleListReturn'
export type ListReturnSnapshot = { key: string; owner: string | null; articles: ArticleListItem[]; offset: number; hasMore: boolean; scrollY: number }
export function saveListReturn(snapshot: Omit<ListReturnSnapshot, 'owner'>) {
  try { sessionStorage.setItem(KEY, JSON.stringify({ ...snapshot, owner: localStorage.getItem('user') })) } catch {}
}
export function takeListReturn(key: string): ListReturnSnapshot | null {
  try {
    const raw = sessionStorage.getItem(KEY)
    sessionStorage.removeItem(KEY)
    if (!raw || !sessionStorage.getItem('articleListScroll')) return null
    sessionStorage.removeItem('articleListScroll')
    const value: ListReturnSnapshot = JSON.parse(raw)
    if (value.key !== key || value.owner !== localStorage.getItem('user') || !Array.isArray(value.articles)
      || !Number.isFinite(value.scrollY) || !Number.isFinite(value.offset)) return null
    return value
  } catch { return null }
}
