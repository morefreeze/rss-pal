import { act, renderHook } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { useExploreArticleActions } from '../src/hooks/useExploreArticleActions'
import { getUser, updateExploreArticleState } from '../src/api/client'
vi.mock('../src/api/client', () => ({ updateExploreArticleState: vi.fn(), getUser: vi.fn(() => ({id:1})) }))
it('guards duplicate clicks and stale route completions', async () => {
  const update = vi.mocked(updateExploreArticleState)
  let resolve!: (value: {saved:boolean; skipped:boolean}) => void
  update.mockImplementationOnce(() => new Promise(r => { resolve = r }))
  const skipped = vi.fn()
  const { result, rerender } = renderHook(({id}) => useExploreArticleActions(id, false, skipped), {initialProps:{id:1}})
  act(() => { void result.current.skip(); void result.current.toggleSaved() })
  expect(update).toHaveBeenCalledTimes(1)
  rerender({id:2})
  await act(async () => resolve({saved:false,skipped:true}))
  expect(skipped).not.toHaveBeenCalled()
  expect(result.current.pending).toBe(false)
  update.mockRejectedValueOnce(new Error('offline'))
  await act(async () => { await result.current.toggleSaved() })
  expect(result.current.saved).toBe(false)
  expect(result.current.error).toBeTruthy()
})
it('saves without leaving and undoes a skip after unmount', async () => {
  const update = vi.mocked(updateExploreArticleState)
  update.mockResolvedValue({saved:true,skipped:false})
  const skipped = vi.fn()
  let undo: (() => void) | undefined
  const listener = (event: Event) => { undo = (event as CustomEvent).detail.action?.onClick }
  window.addEventListener('show-toast', listener)
  const { result, unmount } = renderHook(() => useExploreArticleActions(3, false, skipped))
  await act(async () => { await result.current.toggleSaved() })
  expect(result.current.saved).toBe(true)
  expect(skipped).not.toHaveBeenCalled()
  await act(async () => { await result.current.skip() })
  expect(skipped).toHaveBeenCalledOnce()
  unmount()
  await act(async () => { undo?.() })
  expect(update).toHaveBeenLastCalledWith(3, {skipped:false})
  window.removeEventListener('show-toast', listener)
})

it('does not apply a global skip undo to a different signed-in account', async () => {
  const update = vi.mocked(updateExploreArticleState)
  update.mockClear().mockResolvedValue({saved:false, skipped:true})
  let undo: (() => void) | undefined
  const listener = (event: Event) => { undo = (event as CustomEvent).detail.action?.onClick }
  window.addEventListener('show-toast', listener)
  const { result, unmount } = renderHook(() => useExploreArticleActions(9, false))
  await act(async () => { await result.current.skip() })
  expect(undo).toBeTypeOf('function')
  unmount()
  vi.mocked(getUser).mockReturnValue({id:2} as ReturnType<typeof getUser>)
  await act(async () => { undo?.() })
  expect(update).toHaveBeenCalledTimes(1)
  window.removeEventListener('show-toast', listener)
})
