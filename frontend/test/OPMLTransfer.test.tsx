import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import OPMLTransfer from '../src/components/OPMLTransfer'

const api = vi.hoisted(() => ({ getFeeds: vi.fn(), addFeed: vi.fn(), exportOPML: vi.fn() }))
vi.mock('../src/api/client', () => api)
vi.mock('../src/utils/toast', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
beforeEach(() => { vi.resetAllMocks(); api.getFeeds.mockResolvedValue([{ url: 'https://existing.test/feed' }]) })
function upload(text: string) {
  fireEvent.change(screen.getByLabelText('导入 OPML 文件'), { target: { files: [{ size: text.length, text: async () => text }] } })
}
it('shows progress, separates duplicates/failures, preserves title and retries only failures', async () => {
  let finish!: (value: unknown) => void
  api.addFeed.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    .mockRejectedValueOnce({ response: { status: 429, data: { error: '额度不足' } } })
    .mockResolvedValueOnce({ id: 2 })
  const changed = vi.fn().mockResolvedValue(undefined)
  render(<OPMLTransfer onImported={changed} />)
  upload('<opml><body><outline xmlUrl="https://existing.test/feed"/><outline title="My title" xmlUrl="https://new.test/feed"/><outline xmlUrl="https://new.test/feed"/><outline text="Retry" xmlUrl="https://retry.test/feed"/></body></opml>')
  await waitFor(() => expect(api.addFeed).toHaveBeenCalledTimes(1))
  expect(screen.getByRole('progressbar')).toBeTruthy()
  expect(api.addFeed.mock.calls[0]).toEqual(['https://new.test/feed', 'rss', false, 'My title'])
  await act(async () => finish({ id: 1 }))
  await screen.findByText('成功 1 · 重复 2 · 失败 1')
  expect(screen.getByText(/额度不足/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '重试失败项' }))
  await screen.findByText('成功 2 · 重复 2 · 失败 0')
  expect(api.addFeed).toHaveBeenCalledTimes(3)
  expect(api.addFeed.mock.calls[2][0]).toBe('https://retry.test/feed')
})
it('rejects malformed input without submitting subscriptions', async () => {
  render(<OPMLTransfer onImported={vi.fn()} />)
  upload('<opml><body><outline xmlUrl="https://x.test"/>')
  await screen.findByRole('alert')
  expect(api.addFeed).not.toHaveBeenCalled()
})
it('counts a racing 409 as duplicate and exposes invalid URL without sending it', async () => {
  api.addFeed.mockRejectedValueOnce({ response: { status: 409 } })
  render(<OPMLTransfer onImported={vi.fn()} />)
  upload('<opml><body><outline xmlUrl="https://race.test"/><outline xmlUrl="file:///etc/passwd"/></body></opml>')
  await screen.findByText('成功 0 · 重复 1 · 失败 1')
  expect(api.addFeed).toHaveBeenCalledTimes(1)
  expect(screen.queryByRole('button', { name: '重试失败项' })).toBeNull()
})
it('deduplicates file entries even when the first attempt fails', async () => {
  api.addFeed.mockRejectedValue({ response: { status: 429, data: { error: '额度不足' } } })
  render(<OPMLTransfer onImported={vi.fn()} />)
  upload('<opml><body><outline xmlUrl="https://same.test/feed"/><outline xmlUrl="https://same.test/feed"/></body></opml>')
  await screen.findByText('成功 0 · 重复 1 · 失败 1')
  expect(api.addFeed).toHaveBeenCalledTimes(1)
})
it('recognizes an uncertain previous success on retry by refreshing subscriptions', async () => {
  api.addFeed.mockRejectedValueOnce(new Error('网络中断'))
  render(<OPMLTransfer onImported={vi.fn()} />)
  upload('<opml><body><outline xmlUrl="https://uncertain.test/feed"/></body></opml>')
  await screen.findByText('成功 0 · 重复 0 · 失败 1')
  api.getFeeds.mockResolvedValue([{ url: 'https://uncertain.test/feed' }])
  fireEvent.click(screen.getByRole('button', { name: '重试失败项' }))
  await screen.findByText('成功 0 · 重复 1 · 失败 0')
  expect(api.addFeed).toHaveBeenCalledTimes(1)
})
it('rejects an oversized file before reading it', async () => {
  render(<OPMLTransfer onImported={vi.fn()} />)
  const text = vi.fn()
  fireEvent.change(screen.getByLabelText('导入 OPML 文件'), { target: { files: [{ size: 6 * 1024 * 1024, text }] } })
  await screen.findByRole('alert')
  expect(text).not.toHaveBeenCalled()
  expect(api.addFeed).not.toHaveBeenCalled()
})
