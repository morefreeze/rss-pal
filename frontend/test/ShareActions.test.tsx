import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import ShareActions from '../src/components/ShareActions'
const input = { title: '标题', summaryBrief: 'AI 总结', content: '正文开头', shareURL: 'https://r.example/abc' }
beforeEach(() => {
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue(undefined) } })
  Object.defineProperty(navigator, 'share', { configurable: true, value: undefined })
})
it('copies composed text and separately supports URL-only copy', async () => {
  render(<ShareActions {...input} />)
  fireEvent.click(screen.getByRole('button', {name: '复制分享文案'}))
  await screen.findByText('分享文案已复制')
  expect(navigator.clipboard.writeText).toHaveBeenLastCalledWith('标题\n\nAI 总结\n\nhttps://r.example/abc')
  fireEvent.click(screen.getByRole('button', {name: '复制链接'}))
  await screen.findByText('链接已复制')
  expect(navigator.clipboard.writeText).toHaveBeenLastCalledWith(input.shareURL)
})
it('invokes native share directly and does not copy on cancellation', async () => {
  const share = vi.fn().mockRejectedValue(new DOMException('cancel', 'AbortError'))
  Object.defineProperty(navigator, 'share', { configurable: true, value: share })
  render(<ShareActions {...input} />)
  fireEvent.click(screen.getByRole('button', {name: '更多分享'}))
  expect(share).toHaveBeenCalledTimes(1)
  const data = share.mock.calls[0][0]
  expect(data.text).toContain('AI 总结')
  expect(data.url).toBe(input.shareURL)
  expect(data.text).not.toContain(input.shareURL)
  await waitFor(() => expect((screen.getByRole('button', {name: '更多分享'}) as HTMLButtonElement).disabled).toBe(false))
  expect(navigator.clipboard.writeText).not.toHaveBeenCalled()
})
it('offers local QR and copy instructions for WeChat without native sharing', async () => {
  render(<ShareActions {...input} />)
  fireEvent.click(screen.getByRole('button', {name: '分享到微信'}))
  expect(await screen.findByRole('img', {name: '微信扫码阅读文章'})).toBeTruthy()
  expect(screen.getByText(/微信扫一扫/)).toBeTruthy()
  await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalled())
})
it('exposes selectable text when share and clipboard fail', async () => {
  Object.defineProperty(navigator, 'share', {configurable: true, value: vi.fn().mockRejectedValue(new Error('denied'))})
  vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('denied'))
  render(<ShareActions {...input} />)
  fireEvent.click(screen.getByRole('button', {name: '更多分享'}))
  const text = await screen.findByRole('textbox', {name: '手动复制分享文案'}) as HTMLTextAreaElement
  expect(text.value).toContain(input.shareURL)
  expect(text.value).toContain('AI 总结')
  expect(document.activeElement).toBe(text)
})

it('keeps QR available after cancelling a WeChat chooser without copying', async () => {
  Object.defineProperty(navigator, 'share', {configurable: true, value: vi.fn().mockRejectedValue(new DOMException('cancel', 'AbortError'))})
  render(<ShareActions {...input} />)
  fireEvent.click(screen.getByRole('button', {name: '分享到微信'}))
  expect(await screen.findByRole('img', {name: '微信扫码阅读文章'})).toBeTruthy()
  await waitFor(() => expect((screen.getByRole('button', {name: '分享到微信'}) as HTMLButtonElement).disabled).toBe(false))
  expect(navigator.clipboard.writeText).not.toHaveBeenCalled()
})
