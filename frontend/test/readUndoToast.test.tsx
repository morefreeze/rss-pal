import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Toaster from '../src/components/Toaster'
import { showReadUndoToast } from '../src/utils/markReadArticles'
const mocks = vi.hoisted(() => ({
  undoMarkRead: vi.fn(),
  invalidateArticleDetail: vi.fn(),
}))
vi.mock('../src/api/client', () => ({ undoMarkRead: mocks.undoMarkRead }))
vi.mock('../src/api/articleDetailCache', () => ({
  invalidateArticleDetail: mocks.invalidateArticleDetail,
}))
afterEach(() => {
  vi.useRealTimers()
  sessionStorage.clear()
  localStorage.clear()
  vi.clearAllMocks()
})
describe('read undo toast', () => {
  it('offers undo for five seconds and restores only IDs actually undone', async () => {
    vi.useFakeTimers()
    sessionStorage.setItem('readArticles', '[1,2,3]')
    mocks.undoMarkRead.mockResolvedValue({ count: 1, article_ids: [2] })
    const refresh = vi.fn()
    window.addEventListener('read-state-refresh', refresh)
    render(<Toaster />)
    act(() =>
      showReadUndoToast({ count: 2, article_ids: [2, 3], undo_token: 'token' }),
    )
    act(() => vi.advanceTimersByTime(4999))
    expect(screen.getByRole('button', { name: '撤销' })).toBeTruthy()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '撤销' }))
    })
    expect(mocks.undoMarkRead).toHaveBeenCalledWith('token')
    expect(JSON.parse(sessionStorage.getItem('readArticles')!)).toEqual([1, 3])
    expect(mocks.invalidateArticleDetail).toHaveBeenCalledWith(2)
    expect(refresh).toHaveBeenCalledTimes(1)
    window.removeEventListener('read-state-refresh', refresh)
  })
  it('dismisses at five seconds without sending an undo request', () => {
    vi.useFakeTimers()
    render(<Toaster />)
    act(() =>
      showReadUndoToast({ count: 1, article_ids: [1], undo_token: 'token' }),
    )
    act(() => vi.advanceTimersByTime(5000))
    expect(screen.queryByRole('button', { name: '撤销' })).toBeNull()
    expect(mocks.undoMarkRead).not.toHaveBeenCalled()
  })
  it('does not offer undo for a zero-change action', () => {
    render(<Toaster />)
    act(() => showReadUndoToast({ count: 0, article_ids: [], undo_token: '' }))
    expect(screen.queryByRole('button', { name: '撤销' })).toBeNull()
  })
  it('does not replay a toast after switching accounts', async () => {
    localStorage.setItem('user', '{"id":1}')
    render(<Toaster />)
    act(() => showReadUndoToast({count:1,article_ids:[1],undo_token:'token'}))
    localStorage.setItem('user', '{"id":2}')
    await act(async () => {fireEvent.click(screen.getByRole('button',{name:'撤销'}))})
    expect(mocks.undoMarkRead).not.toHaveBeenCalled()
    expect(screen.getByText('账号已切换，请刷新后重试')).toBeTruthy()
  })
  it('refreshes after an exhausted token without offering an endless retry', async () => {
    mocks.undoMarkRead.mockRejectedValue({isAxiosError:true,response:{status:404}})
    render(<Toaster />)
    act(() => showReadUndoToast({count:1,article_ids:[1],undo_token:'token'}))
    await act(async () => {fireEvent.click(screen.getByRole('button',{name:'撤销'}))})
    expect(screen.getByText('撤销已过期或已使用，请刷新列表')).toBeTruthy()
    expect(screen.queryByRole('button',{name:'重试'})).toBeNull()
  })

})
