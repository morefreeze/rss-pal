import { useCallback, useEffect, useMemo, useState } from 'react'
import { getArticleCaptureDrafts, removeArticleCaptureDrafts } from '../api/client'
import {
  addDraftTargets, enrichDraftLinks, loadDraftLinks, removeDraftURLs, saveDraftLinks,
  type DraftLink, type DraftTarget,
} from '../utils/linkSetSelection'
import { normalizeHTTPURL } from '../utils/url'

// Local selections remain an offline scratchpad. Shortcut imports are durable
// server rows, merged into that same reader list without a second queue UI.
export function useArticleCaptureDrafts(articleId: number, fetchedURLs: ReadonlySet<string>) {
  const scope = useMemo(() => ({
    articleId, active: false, epoch: 0, request: 0,
    remoteURLs: new Set<string>(), removed: new Set<string>(), submitted: new Set<string>(),
    pending: new Set<string>(), retryDeletes: new Set<string>(),
  }), [articleId])
  const [state, setState] = useState(() => ({
    articleId, local: loadDraftLinks(articleId), remote: [] as DraftLink[], fetched: [] as string[],
  }))
  const [error, setError] = useState<string | null>(null)
  const current = state.articleId === articleId ? state : undefined
  const fetchedURLSet = useMemo(() => new Set([
    ...fetchedURLs, ...(current?.fetched ?? []), ...scope.submitted,
  ]), [fetchedURLs, current?.fetched, scope])
  const draftLinks = useMemo(() => {
    let merged = current?.local ?? []
    for (const draft of current?.remote ?? []) {
      merged = addDraftTargets(merged, [draft], fetchedURLSet, draft.addedAt)
    }
    return removeDraftURLs(merged, fetchedURLSet)
  }, [current, fetchedURLSet])

  useEffect(() => {
    scope.active = true
    setError(null)
    setState({ articleId, local: loadDraftLinks(articleId), remote: [], fetched: [] })
    let cancelled = false
    const refresh = async () => {
      if (document.visibilityState === 'hidden' || scope.pending.size > 0) return
      const request = ++scope.request
      const epoch = scope.epoch
      try {
        // A local selection can be removed offline before its remote copy is
        // discovered. Retry that best-effort delete on the next visible sync.
        const retry = [...scope.retryDeletes]
        if (retry.length > 0) {
          try {
            await removeArticleCaptureDrafts(articleId, retry)
            if (cancelled || epoch !== scope.epoch) return
            retry.forEach(url => scope.retryDeletes.delete(url))
          } catch { /* keep the local removal and retry after reconnecting */ }
        }
        const data = await getArticleCaptureDrafts(articleId)
        if (cancelled || request !== scope.request || epoch !== scope.epoch || scope.pending.size > 0) return
        const remote = data.drafts.filter(draft => {
          const url = normalizeHTTPURL(draft.url)
          return url && !scope.removed.has(url) && !scope.submitted.has(url)
        }).map(draft => ({ ...draft, url: normalizeHTTPURL(draft.url)! }))
        scope.remoteURLs = new Set(remote.map(draft => draft.url))
        // Once a server snapshot confirms deletion, a later intentional share
        // of that URL is eligible again. Failed offline deletes stay suppressed.
        for (const url of scope.removed) {
          if (!scope.retryDeletes.has(url) && !data.drafts.some(draft => normalizeHTTPURL(draft.url) === url)) {
            scope.removed.delete(url)
          }
        }
        setState(previous => previous.articleId === articleId
          ? { ...previous, remote, fetched: data.fetched_link_urls }
          : previous)
      } catch { /* ordinary reading and local selection work while offline */ }
    }
    const wake = () => { void refresh() }
    wake()
    window.addEventListener('focus', wake)
    window.addEventListener('pageshow', wake)
    document.addEventListener('visibilitychange', wake)
    const timer = window.setInterval(wake, 30_000)
    return () => {
      cancelled = true
      scope.active = false
      window.clearInterval(timer)
      window.removeEventListener('focus', wake)
      window.removeEventListener('pageshow', wake)
      document.removeEventListener('visibilitychange', wake)
    }
  }, [articleId, scope])

  useEffect(() => {
    if (!current) return
    saveDraftLinks(articleId, removeDraftURLs(current.local, fetchedURLSet))
  }, [articleId, current?.local, fetchedURLSet])

  const addDrafts = useCallback((links: DraftTarget[]) => {
    const next = addDraftTargets(draftLinks, links, fetchedURLSet)
    const count = next.length - draftLinks.length
    if (!count) return 0
    for (const link of links) {
      const url = normalizeHTTPURL(link.url)
      if (url) { scope.removed.delete(url); scope.retryDeletes.delete(url) }
    }
    setState(previous => previous.articleId === articleId
      ? { ...previous, local: addDraftTargets(previous.local, links, fetchedURLSet) }
      : previous)
    return count
  }, [articleId, draftLinks, fetchedURLSet, scope])

  const enrichDrafts = useCallback((links: DraftTarget[]) => {
    setState(previous => {
      if (previous.articleId !== articleId) return previous
      const local = enrichDraftLinks(previous.local, links)
      const remote = enrichDraftLinks(previous.remote, links)
      return local === previous.local && remote === previous.remote ? previous : { ...previous, local, remote }
    })
  }, [articleId])

  const removeDrafts = useCallback(async (urls: string[]) => {
    const normalized = urls.map(url => normalizeHTTPURL(url)).filter((url): url is string => !!url)
    const removing = normalized.filter(url => !scope.pending.has(url))
    if (!scope.active || removing.length === 0) return
    const knownRemote = removing.some(url => scope.remoteURLs.has(url))
    scope.epoch++
    removing.forEach(url => { scope.removed.add(url); scope.pending.add(url) })
    setError(null)
    const removeRows = () => setState(previous => previous.articleId === articleId ? {
      ...previous,
      local: removeDraftURLs(previous.local, new Set(removing)),
      remote: removeDraftURLs(previous.remote, new Set(removing)),
    } : previous)
    if (!knownRemote) removeRows()
    try {
      await removeArticleCaptureDrafts(articleId, removing)
      // Completion still belongs to this article's saved scratchpad when the
      // reader navigated away while the request was running.
      saveDraftLinks(articleId, removeDraftURLs(loadDraftLinks(articleId), new Set(removing)))
      if (!scope.active) return
      removing.forEach(url => scope.remoteURLs.delete(url))
      removeRows()
    } catch {
      if (!scope.active) return
      if (knownRemote) {
        removing.forEach(url => scope.removed.delete(url))
        setError('移除失败，请联网后重试')
      } else {
        removing.forEach(url => scope.retryDeletes.add(url))
      }
    } finally {
      scope.epoch++
      removing.forEach(url => scope.pending.delete(url))
    }
  }, [articleId, scope])

  const markFetched = useCallback((urls: string[]) => {
    saveDraftLinks(articleId, removeDraftURLs(loadDraftLinks(articleId), new Set(urls)))
    if (!scope.active) return false
    scope.epoch++
    urls.forEach(url => scope.submitted.add(url))
    const removed = new Set(urls)
    setState(previous => previous.articleId === articleId ? {
      ...previous,
      local: removeDraftURLs(previous.local, removed), remote: removeDraftURLs(previous.remote, removed),
      fetched: [...new Set([...previous.fetched, ...urls])],
    } : previous)
    return true
  }, [articleId, scope])

  return { draftLinks, fetchedURLSet, addDrafts, enrichDrafts, removeDrafts, markFetched, error }
}
