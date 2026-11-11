import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import TagBar from '../src/components/TagBar'

const api = vi.hoisted(() => ({
  getArticleTags: vi.fn(), listTags: vi.fn(async () => []),
  addArticleTag: vi.fn(async () => undefined), removeArticleTag: vi.fn(async () => undefined), dismissSuggestion: vi.fn(),
}))
vi.mock('../src/api/client', () => api)
beforeEach(() => {
  vi.clearAllMocks()
  api.getArticleTags.mockResolvedValue({ source: { title: 'Source' }, manual: [{ id: 3, name: 'Existing' }], suggestions: ['Suggested'] })
})

it.each(['add', 'remove', 'adopt'])('refreshes retained lists after successful tag %s', async operation => {
  const listener = vi.fn()
  window.addEventListener('article-content-changed', listener)
  try {
    render(<TagBar articleId={42} />)
    await screen.findByText('Source')
    if (operation === 'add') {
      fireEvent.click(screen.getByRole('button', { name: '+ 添加' }))
      const input = screen.getByPlaceholderText('输入新建或选择已有')
      fireEvent.change(input, { target: { value: 'New tag' } })
      fireEvent.keyDown(input, { key: 'Enter' })
      await waitFor(() => expect(api.addArticleTag).toHaveBeenCalledWith(42, 'New tag'))
    } else if (operation === 'remove') {
      fireEvent.click(screen.getByRole('button', { name: '移除 Existing' }))
      await waitFor(() => expect(api.removeArticleTag).toHaveBeenCalledWith(42, 3))
    } else {
      fireEvent.click(screen.getByRole('button', { name: /Suggested/ }))
      await waitFor(() => expect(api.addArticleTag).toHaveBeenCalledWith(42, 'Suggested'))
    }
    await waitFor(() => expect(listener).toHaveBeenCalledTimes(1))
  } finally { window.removeEventListener('article-content-changed', listener) }
})
