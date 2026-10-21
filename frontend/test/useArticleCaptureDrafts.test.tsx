import { act, renderHook } from '@testing-library/react'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { useArticleCaptureDrafts } from '../src/hooks/useArticleCaptureDrafts'
import { getArticleCaptureDrafts, removeArticleCaptureDrafts } from '../src/api/client'
import { saveDraftLinks } from '../src/utils/linkSetSelection'

vi.mock('../src/api/client', () => ({ getArticleCaptureDrafts: vi.fn(), removeArticleCaptureDrafts: vi.fn() }))
const remote = { url: 'https://example.com/shared', title: 'Shortcut link', addedAt: 123 }
const local = { url: 'https://example.com/manual', title: 'Manual link', addedAt: 100 }
const emptyFetched = new Set<string>()
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(r => { resolve = r })
  return { promise, resolve }
}
beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
  vi.mocked(getArticleCaptureDrafts).mockResolvedValue({ drafts: [], fetched_link_urls: [] })
  vi.mocked(removeArticleCaptureDrafts).mockResolvedValue(undefined)
})
afterEach(() => vi.useRealTimers())

it('merges imported drafts with manual selections and refreshes on focus, pageshow and visible polling', async () => {
  vi.useFakeTimers()
  saveDraftLinks(42, [local])
  const { result } = renderHook(() => useArticleCaptureDrafts(42, emptyFetched))
  await act(async () => {})
  expect(result.current.draftLinks).toEqual([local])
  vi.mocked(getArticleCaptureDrafts).mockResolvedValue({ drafts: [remote], fetched_link_urls: [] })
  await act(async () => { window.dispatchEvent(new Event('focus')) })
  expect(result.current.draftLinks).toEqual([local, remote])
  await act(async () => { window.dispatchEvent(new Event('pageshow')) })
  expect(getArticleCaptureDrafts).toHaveBeenCalledTimes(3)
  await act(async () => { await vi.advanceTimersByTimeAsync(30_000) })
  expect(getArticleCaptureDrafts).toHaveBeenCalledTimes(4)
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
  await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
  expect(getArticleCaptureDrafts).toHaveBeenCalledTimes(4)
})

it('ignores old route replies and never writes old selections into another article', async () => {
  saveDraftLinks(42, [local])
  const pending = deferred<{ drafts: typeof remote[]; fetched_link_urls: string[] }>()
  vi.mocked(getArticleCaptureDrafts).mockReturnValueOnce(pending.promise)
  const { result, rerender } = renderHook(({ id }) => useArticleCaptureDrafts(id, emptyFetched), { initialProps: { id: 42 } })
  rerender({ id: 43 })
  await act(async () => { pending.resolve({ drafts: [remote], fetched_link_urls: [] }) })
  expect(result.current.draftLinks).toEqual([])
  expect(localStorage.getItem('rsspal_batch_sel_43')).toBeNull()
  expect(JSON.parse(localStorage.getItem('rsspal_batch_sel_42')!).links).toEqual([local])
})

it('does not resurrect a removed remote draft from an in-flight reply', async () => {
  vi.mocked(getArticleCaptureDrafts).mockResolvedValue({ drafts: [remote], fetched_link_urls: [] })
  const { result } = renderHook(() => useArticleCaptureDrafts(42, emptyFetched))
  await act(async () => {})
  const stale = deferred<{ drafts: typeof remote[]; fetched_link_urls: string[] }>()
  vi.mocked(getArticleCaptureDrafts).mockReturnValueOnce(stale.promise)
  act(() => window.dispatchEvent(new Event('focus')))
  await act(async () => { await result.current.removeDrafts([remote.url]) })
  expect(removeArticleCaptureDrafts).toHaveBeenCalledWith(42, [remote.url])
  await act(async () => { stale.resolve({ drafts: [remote], fetched_link_urls: [] }) })
  expect(result.current.draftLinks).toEqual([])
})

it('retains remote rows on DELETE failure and preserves offline manual removal', async () => {
  saveDraftLinks(42, [local])
  vi.mocked(getArticleCaptureDrafts).mockResolvedValue({ drafts: [remote], fetched_link_urls: [] })
  const { result } = renderHook(() => useArticleCaptureDrafts(42, emptyFetched))
  await act(async () => {})
  vi.mocked(removeArticleCaptureDrafts).mockRejectedValue(new Error('offline'))
  await act(async () => { await result.current.removeDrafts([remote.url]) })
  expect(result.current.draftLinks).toEqual([local, remote])
  expect(result.current.error).toBe('移除失败，请联网后重试')
  await act(async () => { await result.current.removeDrafts([local.url]) })
  expect(result.current.draftLinks).toEqual([remote])
})

it('keeps local removal when the same remote draft was still loading and DELETE fails offline', async () => {
  saveDraftLinks(42, [local])
  const pending = deferred<{ drafts: typeof remote[]; fetched_link_urls: string[] }>()
  vi.mocked(getArticleCaptureDrafts).mockReturnValueOnce(pending.promise)
  vi.mocked(removeArticleCaptureDrafts).mockRejectedValueOnce(new Error('offline'))
  const { result } = renderHook(() => useArticleCaptureDrafts(42, emptyFetched))
  await act(async () => { await result.current.removeDrafts([local.url]) })
  await act(async () => { pending.resolve({ drafts: [local], fetched_link_urls: [] }) })
  expect(result.current.draftLinks).toEqual([])
  expect(removeArticleCaptureDrafts).toHaveBeenCalledWith(42, [local.url])
})

it('immediately excludes submitted URLs from late syncs even before article detail refresh', async () => {
  vi.mocked(getArticleCaptureDrafts).mockResolvedValue({ drafts: [remote], fetched_link_urls: [] })
  const { result } = renderHook(() => useArticleCaptureDrafts(42, emptyFetched))
  await act(async () => {})
  const stale = deferred<{ drafts: typeof remote[]; fetched_link_urls: string[] }>()
  vi.mocked(getArticleCaptureDrafts).mockReturnValueOnce(stale.promise)
  act(() => window.dispatchEvent(new Event('focus')))
  act(() => result.current.markFetched([remote.url]))
  await act(async () => { stale.resolve({ drafts: [remote], fetched_link_urls: [] }) })
  await act(async () => { window.dispatchEvent(new Event('focus')) })
  expect(result.current.draftLinks).toEqual([])
  expect(result.current.fetchedURLSet.has(remote.url)).toBe(true)
})

it('prunes fetched remote URLs and local selections without persisting imported rows locally', async () => {
  saveDraftLinks(42, [local])
  vi.mocked(getArticleCaptureDrafts).mockResolvedValue({ drafts: [remote, local], fetched_link_urls: [local.url] })
  const { result } = renderHook(() => useArticleCaptureDrafts(42, emptyFetched))
  await act(async () => {})
  expect(result.current.draftLinks).toEqual([remote])
  expect(localStorage.getItem('rsspal_batch_sel_42')).toBeNull()
})

it('ignores pending mutation success after changing routes', async () => {
  saveDraftLinks(43, [local])
  vi.mocked(getArticleCaptureDrafts).mockResolvedValueOnce({ drafts: [remote], fetched_link_urls: [] })
  const pending = deferred<void>()
  vi.mocked(removeArticleCaptureDrafts).mockReturnValueOnce(pending.promise)
  const { result, rerender } = renderHook(({id}) => useArticleCaptureDrafts(id, emptyFetched), { initialProps: {id:42} })
  await act(async () => {})
  let removal!: Promise<void>
  act(() => { removal = result.current.removeDrafts([remote.url]) })
  rerender({id:43})
  await act(async () => { pending.resolve(); await removal })
  expect(result.current.draftLinks).toEqual([local])
  expect(result.current.error).toBeNull()
})

it('retries a failed offline local deletion on the next visible sync', async () => {
  saveDraftLinks(42, [local])
  vi.mocked(getArticleCaptureDrafts).mockResolvedValue({drafts:[],fetched_link_urls:[]})
  vi.mocked(removeArticleCaptureDrafts).mockRejectedValueOnce(new Error('offline'))
  const { result } = renderHook(() => useArticleCaptureDrafts(42, emptyFetched))
  await act(async () => {})
  await act(async () => { await result.current.removeDrafts([local.url]) })
  await act(async () => { document.dispatchEvent(new Event('visibilitychange')) })
  expect(removeArticleCaptureDrafts).toHaveBeenCalledTimes(2)
  expect(result.current.draftLinks).toEqual([])
})

it('ignores a late sync after unmount and removes all wake-up listeners', async () => {
  const pending = deferred<{ drafts: typeof remote[]; fetched_link_urls: string[] }>()
  vi.mocked(getArticleCaptureDrafts).mockReturnValueOnce(pending.promise)
  const { unmount } = renderHook(() => useArticleCaptureDrafts(42, emptyFetched))
  unmount()
  await act(async () => { pending.resolve({drafts:[remote],fetched_link_urls:[]}) })
  act(() => {
    window.dispatchEvent(new Event('focus'))
    window.dispatchEvent(new Event('pageshow'))
    document.dispatchEvent(new Event('visibilitychange'))
  })
  expect(getArticleCaptureDrafts).toHaveBeenCalledTimes(1)
  expect(localStorage.getItem('rsspal_batch_sel_42')).toBeNull()
})

it('clears persisted submitted selections even if the batch finishes after navigation', async () => {
  saveDraftLinks(42, [local])
  const { result, rerender } = renderHook(({id}) => useArticleCaptureDrafts(id, emptyFetched), { initialProps: {id:42} })
  const completeOldBatch = result.current.markFetched
  rerender({id:43})
  act(() => { completeOldBatch([local.url]) })
  expect(result.current.draftLinks).toEqual([])
  rerender({id:42})
  expect(result.current.draftLinks).toEqual([])
})

it('clears the original persisted duplicate after a remote removal finishes on another route', async () => {
  saveDraftLinks(42, [remote])
  vi.mocked(getArticleCaptureDrafts).mockResolvedValueOnce({drafts:[remote],fetched_link_urls:[]})
  const pending = deferred<void>()
  vi.mocked(removeArticleCaptureDrafts).mockReturnValueOnce(pending.promise)
  const { result, rerender } = renderHook(({id}) => useArticleCaptureDrafts(id, emptyFetched), { initialProps: {id:42} })
  await act(async () => {})
  let removal!: Promise<void>
  act(() => { removal = result.current.removeDrafts([remote.url]) })
  rerender({id:43})
  await act(async () => { pending.resolve(); await removal })
  rerender({id:42})
  expect(result.current.draftLinks).toEqual([])
})
