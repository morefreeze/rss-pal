import { useEffect, useRef, useState } from 'react'
import { getUser, updateExploreArticleState } from '../api/client'
import { toast } from '../utils/toast'

export const EXPLORE_STATE_CHANGED = 'explore-article-state-changed'
const notify = (id: number, state: { saved: boolean; skipped: boolean; normalized_url?: string }) =>
  window.dispatchEvent(new CustomEvent(EXPLORE_STATE_CHANGED, { detail: { id, ...state } }))

export function useExploreArticleActions(id: number, initialSaved = false, onSkipped?: () => void, initialSkipped = false) {
  const [saved, setSaved] = useState(initialSaved)
  const [skipped, setSkipped] = useState(initialSkipped)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const busy = useRef(false)
  const generation = useRef(0)
  useEffect(() => {
    ++generation.current
    busy.current = false
    setPending(false)
    setError('')
    setSaved(initialSaved)
    setSkipped(initialSkipped)
    return () => { ++generation.current }
  }, [id, initialSaved, initialSkipped])

  const change = async (patch: { saved?: boolean; skipped?: boolean }) => {
    if (busy.current || id <= 0) return
    const ownerID = getUser()?.id
    const sameOwner = () => getUser()?.id === ownerID
    busy.current = true
    const current = generation.current
    setPending(true)
    setError('')
    try {
      const state = await updateExploreArticleState(id, patch)
      if (current !== generation.current || !sameOwner()) return
      setSaved(state.saved)
      setSkipped(state.skipped)
      // Save stays in place; the list applies this event without losing pagination.
      if (patch.skipped) {
        let undoPending = false
        const undo = async () => {
          if (undoPending || !sameOwner()) return
          undoPending = true
          try {
            const restored = await updateExploreArticleState(id, { skipped: false })
            if (sameOwner()) notify(id, restored)
          } catch {
            if (sameOwner()) toast.error('撤销失败，请重试', { action: { label: '重试', onClick: () => { void undo() } } })
          } finally { undoPending = false }
        }
        toast.info('已略过这篇文章', { durationMs: 8000, action: { label: '撤销', onClick: () => { void undo() } } })
        onSkipped?.()
      }
      notify(id, state)
    } catch {
      if (current !== generation.current || !sameOwner()) return
      setError('操作失败，请重试')
      toast.error('操作失败，请重试')
    } finally {
      if (current === generation.current && sameOwner()) { busy.current = false; setPending(false) }
    }
  }
  return { saved, skipped, pending, error, skip: () => skipped ? Promise.resolve() : change({ skipped: true }), toggleSaved: () => change({ saved: !saved }) }
}
