import { undoMarkRead, type ReadMutationResponse } from '../api/client'
import { toast } from './toast'
import { isAxiosError } from 'axios'
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

function recordUnreadArticles(ids: number[]) {
  const undone = new Set(ids)
  for (const id of ids) invalidateArticleDetail(id)
  try {
    const stored: number[] = JSON.parse(
      sessionStorage.getItem('readArticles') || '[]',
    )
    sessionStorage.setItem(
      'readArticles',
      JSON.stringify(stored.filter((id) => !undone.has(id))),
    )
  } catch {
    /* Storage can be unavailable in private browsing. */
  }
  window.dispatchEvent(new Event('refresh-unread'))
}

export function showReadUndoToast(result: ReadMutationResponse) {
  const owner = localStorage.getItem('user')
  let pending = false
  const undo = async () => {
    if (pending) return
    if (owner !== localStorage.getItem('user')) {
      toast.error('账号已切换，请刷新后重试')
      return
    }
    pending = true
    try {
      const undone = await undoMarkRead(result.undo_token)
      if (owner !== localStorage.getItem('user')) return
      recordUnreadArticles(undone.article_ids)
      window.dispatchEvent(new Event('read-state-refresh'))
      toast.success(`已撤销 ${undone.count} 篇文章的已读标记`)
    } catch (error) {
      // A lost response may hide a successful server undo. Drop the session
      // overlay and revalidate server state before retrying the same token.
      if (owner !== localStorage.getItem('user')) return
      recordUnreadArticles(result.article_ids)
      window.dispatchEvent(new Event('read-state-refresh'))
      if (isAxiosError(error) && error.response?.status === 404)
        toast.error('撤销已过期或已使用，请刷新列表')
      else
        toast.error('撤销失败，请重试', {
          durationMs: 5000,
          action: {
            label: '重试',
            onClick: () => {
              void undo()
            },
          },
        })
    } finally {
      pending = false
    }
  }
  toast.success(`已将 ${result.count} 篇文章标记为已读`, {
    durationMs: 5000,
    ...(result.undo_token && result.count > 0
      ? {
          action: {
            label: '撤销',
            onClick: () => {
              void undo()
            },
          },
        }
      : {}),
  })
}
