import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { batchFetchCandidates } from '../src/api/client'
import { BatchFetchConfirmDialog } from '../src/components/BatchFetchConfirmDialog'
import type { DraftLink } from '../src/utils/linkSetSelection'

vi.mock('../src/api/client', () => ({ batchFetchCandidates: vi.fn() }))

const mockedBatchFetch = vi.mocked(batchFetchCandidates)

const drafts: DraftLink[] = [
  { url: 'https://a.example/', title: 'A title', addedAt: 1 },
  { url: 'https://b.example/', title: 'B title', addedAt: 2 },
  { url: 'https://c.example/', title: 'C title', addedAt: 3 },
]

function renderDialog(overrides: Partial<React.ComponentProps<typeof BatchFetchConfirmDialog>> = {}) {
  const props: React.ComponentProps<typeof BatchFetchConfirmDialog> = {
    open: true,
    articleId: 7,
    drafts,
    fetchedURLs: new Set(['https://b.example/']),
    onRemove: vi.fn(),
    onClose: vi.fn(),
    onFetched: vi.fn(),
    ...overrides,
  }
  return { ...render(<BatchFetchConfirmDialog {...props} />), props }
}

beforeEach(() => mockedBatchFetch.mockReset())

describe('BatchFetchConfirmDialog drafts', () => {
  it('keeps draft order, checks selectable rows, and disables fetched rows', () => {
    renderDialog()
    const rows = screen.getAllByTestId('draft-row')
    expect(rows.map((row) => within(row).getByText(/ title/).textContent)).toEqual([
      'A title', 'B title', 'C title',
    ])
    const boxes = screen.getAllByRole('checkbox') as HTMLInputElement[]
    expect(boxes.map((box) => [box.checked, box.disabled])).toEqual([
      [true, false],
      [false, true],
      [true, false],
    ])
  })

  it('supports all, invert, and none for this submission', () => {
    renderDialog()
    const checked = () => (screen.getAllByRole('checkbox') as HTMLInputElement[]).map((box) => box.checked)
    fireEvent.click(screen.getByRole('button', { name: '取消全选' }))
    expect(checked()).toEqual([false, false, false])
    fireEvent.click(screen.getByRole('button', { name: '全选' }))
    expect(checked()).toEqual([true, false, true])
    fireEvent.click(screen.getByRole('button', { name: '反选' }))
    expect(checked()).toEqual([false, false, false])
  })

  it('permanently removes a draft through the parent callback', () => {
    const onRemove = vi.fn()
    renderDialog({ onRemove })
    fireEvent.click(screen.getByRole('button', { name: '从草稿移除 A title' }))
    expect(onRemove).toHaveBeenCalledWith('https://a.example/')
  })

  it('submits checked URLs in display order and reports the accepted URLs', async () => {
    mockedBatchFetch.mockResolvedValue({ inserted: 1 })
    const onFetched = vi.fn()
    const onClose = vi.fn()
    renderDialog({ onFetched, onClose })
    fireEvent.click(screen.getByRole('checkbox', { name: '本次抓取 C title' }))
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '开始抓取（1）' }))
      await Promise.resolve()
    })

    await waitFor(() => expect(mockedBatchFetch).toHaveBeenCalledWith(7, [
      { title: 'A title', url: 'https://a.example/' },
    ]))
    expect(onFetched).toHaveBeenCalledWith(['https://a.example/'], 1)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('retains the dialog, error, and checkbox state when submission fails', async () => {
    const failedResult = Object.defineProperty({}, 'inserted', {
      get: () => {
        throw Object.assign(new Error('request failed'), {
          response: { data: { error: '服务拒绝' } },
        })
      },
    }) as { inserted: number }
    mockedBatchFetch.mockResolvedValue(failedResult)
    const onClose = vi.fn()
    renderDialog({ onClose })
    fireEvent.click(screen.getByRole('checkbox', { name: '本次抓取 C title' }))
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '开始抓取（1）' }))
      await Promise.resolve()
    })

    expect(await screen.findByText('服务拒绝')).toBeTruthy()
    expect(mockedBatchFetch).toHaveBeenCalledTimes(1)
    expect(onClose).not.toHaveBeenCalled()
    expect((screen.getByRole('checkbox', { name: '本次抓取 A title' }) as HTMLInputElement).checked).toBe(true)
    expect((screen.getByRole('checkbox', { name: '本次抓取 C title' }) as HTMLInputElement).checked).toBe(false)
    expect(screen.getByRole('dialog')).toBeTruthy()
  })
})
