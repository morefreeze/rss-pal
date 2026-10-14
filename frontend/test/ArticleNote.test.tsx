import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import ArticleNote from '../src/components/ArticleNote'
import { updateArticleNote } from '../src/api/client'
vi.mock('../src/api/client', () => ({ updateArticleNote: vi.fn() }))

describe('ArticleNote', () => {
  it('shows the private note and saves freeform edits including empty text', async () => {
    vi.mocked(updateArticleNote).mockResolvedValue({ capture_note: '' })
    const saved = vi.fn()
    render(<ArticleNote articleId={8} note="群里推荐，想试试这个方法" onSaved={saved} />)
    expect(screen.getByText('群里推荐，想试试这个方法')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '编辑随手记' }))
    fireEvent.change(screen.getByRole('textbox', { name: '我的随手记' }), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: '保存随手记' }))
    await waitFor(() => expect(saved).toHaveBeenCalledWith(''))
    expect(updateArticleNote).toHaveBeenCalledWith(8, '')
  })
  it('retains input on failure and supports command-enter retry', async () => {
    vi.mocked(updateArticleNote).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ capture_note: '新的想法' })
    const saved = vi.fn()
    render(<ArticleNote articleId={9} note="" onSaved={saved} />)
    fireEvent.click(screen.getByRole('button', { name: '记下想法' }))
    const input = screen.getByRole('textbox', { name: '我的随手记' })
    fireEvent.change(input, { target: { value: '新的想法' } })
    fireEvent.keyDown(input, { key: 'Enter', metaKey: true })
    await screen.findByRole('alert')
    expect((input as HTMLTextAreaElement).value).toBe('新的想法')
    fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true })
    await waitFor(() => expect(saved).toHaveBeenCalledWith('新的想法'))
  })
  it('does not submit IME composition or overlong input', async () => {
    vi.mocked(updateArticleNote).mockClear()
    render(<ArticleNote articleId={9} note="" onSaved={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: '记下想法' }))
    const input = screen.getByRole('textbox', { name: '我的随手记' })
    fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true, isComposing: true })
    expect(updateArticleNote).not.toHaveBeenCalled()
    fireEvent.change(input, { target: { value: '想'.repeat(10001) } })
    expect((screen.getByRole('button', { name: '保存随手记' }) as HTMLButtonElement).disabled).toBe(true)
  })
})
