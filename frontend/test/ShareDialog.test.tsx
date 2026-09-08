// @vitest-environment-options { "url": "https://rss.example/articles/42" }
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ShareDialog } from '../src/components/ShareDialog'
import type { ArticleShareListItem } from '../src/api/client'

const apiMocks = vi.hoisted(() => ({
  listArticleShares: vi.fn(),
  createArticleShare: vi.fn(),
  revokeArticleShare: vi.fn(),
}))

vi.mock('../src/api/client', () => apiMocks)

function share(
  id: string,
  status: ArticleShareListItem['status'] = 'active',
  overrides: Partial<ArticleShareListItem> = {},
): ArticleShareListItem {
  return {
    id,
    url: `/share/signed-${id}`,
    created_at: '2026-09-08T12:00:00Z',
    expires_at: status === 'expired' ? '2026-09-09T12:00:00Z' : null,
    status,
    legacy: false,
    ...overrides,
  }
}

const activeShare = (id: string, url = `/share/signed-${id}`) => share(id, 'active', { url })
const expiredShare = (id: string) => share(id, 'expired')
const revokedShare = (id: string) => share(id, 'revoked')
const legacyShare = (id: string) => share(id, 'active', { legacy: true })

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function renderShareDialog(overrides: Partial<React.ComponentProps<typeof ShareDialog>> = {}) {
  const props: React.ComponentProps<typeof ShareDialog> = {
    articleId: 42,
    title: 'A useful article',
    open: true,
    onClose: vi.fn(),
    onCopyXiaohongshu: vi.fn(),
    onExportMarkdown: vi.fn(),
    ...overrides,
  }
  return { ...render(<ShareDialog {...props} />), props }
}

describe('ShareDialog', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/articles/42')
    apiMocks.listArticleShares.mockReset().mockResolvedValue([])
    apiMocks.createArticleShare.mockReset()
    apiMocks.revokeArticleShare.mockReset()
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
    })
  })

  it('loads only while open, shows loading, and loads again after reopening', async () => {
    const first = deferred<ArticleShareListItem[]>()
    apiMocks.listArticleShares.mockReturnValueOnce(first.promise).mockResolvedValueOnce([])
    const view = renderShareDialog({ open: false })
    expect(apiMocks.listArticleShares).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()

    view.rerender(<ShareDialog {...view.props} open />)
    expect(screen.getByText('正在加载分享链接…')).toBeTruthy()
    expect(apiMocks.listArticleShares).toHaveBeenCalledWith(42)
    first.resolve([activeShare('first')])
    expect(await screen.findByText('first')).toBeTruthy()

    view.rerender(<ShareDialog {...view.props} open={false} />)
    view.rerender(<ShareDialog {...view.props} open />)
    await waitFor(() => expect(apiMocks.listArticleShares).toHaveBeenCalledTimes(2))
  })

  it('ignores a stale list response after close and reopen, and reports list errors', async () => {
    const stale = deferred<ArticleShareListItem[]>()
    apiMocks.listArticleShares
      .mockReturnValueOnce(stale.promise)
      .mockRejectedValueOnce({ response: { data: { error: '列表服务不可用' } } })
    const view = renderShareDialog()
    view.rerender(<ShareDialog {...view.props} open={false} />)
    view.rerender(<ShareDialog {...view.props} open />)
    expect(await screen.findByText('列表服务不可用')).toBeTruthy()
    stale.resolve([activeShare('stale')])
    await Promise.resolve()
    expect(screen.queryByText('stale')).toBeNull()
  })

  it('creates a permanent link, absolutizes it, copies it, and keeps the dialog open', async () => {
    const created = activeShare('new-id', '/share/signed')
    apiMocks.createArticleShare.mockResolvedValue(created)
    const user = userEvent.setup()
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)
    renderShareDialog()

    await user.click(await screen.findByRole('button', { name: '创建新链接' }))
    expect(apiMocks.createArticleShare).toHaveBeenCalledWith(42, null)
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(
      new URL('/share/signed', window.location.origin).toString(),
    ))
    expect(screen.getByText('new-id')).toBeTruthy()
    expect(screen.getByRole('dialog')).toBeTruthy()
  })

  it('creates 7-day and 30-day links with future ISO expiries', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(new Date('2026-09-08T12:00:00Z').getTime())
    apiMocks.createArticleShare
      .mockResolvedValueOnce(activeShare('seven'))
      .mockResolvedValueOnce(activeShare('thirty'))
    const user = userEvent.setup()
    renderShareDialog()
    await screen.findByText('还没有分享链接')

    await user.click(screen.getByRole('radio', { name: '7 天' }))
    await user.click(screen.getByRole('button', { name: '创建新链接' }))
    expect(apiMocks.createArticleShare).toHaveBeenNthCalledWith(1, 42, '2026-09-15T12:00:00.000Z')
    await user.click(screen.getByRole('radio', { name: '30 天' }))
    await user.click(screen.getByRole('button', { name: '创建新链接' }))
    expect(apiMocks.createArticleShare).toHaveBeenNthCalledWith(2, 42, '2026-10-08T12:00:00.000Z')
  })

  it('converts a datetime-local custom expiry to ISO and rejects missing, invalid, or past values', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(new Date('2026-09-08T12:00:00Z').getTime())
    apiMocks.createArticleShare.mockResolvedValue(activeShare('custom'))
    const user = userEvent.setup()
    renderShareDialog()
    await screen.findByText('还没有分享链接')

    await user.click(screen.getByRole('radio', { name: '自定义' }))
    const submit = screen.getByRole('button', { name: '创建新链接' })
    expect((submit as HTMLButtonElement).disabled).toBe(true)
    const input = screen.getByLabelText('到期时间')
    await user.type(input, '2026-09-08T11:59')
    expect((submit as HTMLButtonElement).disabled).toBe(true)
    await user.clear(input)
    await user.type(input, '2026-09-10T09:30')
    expect((submit as HTMLButtonElement).disabled).toBe(false)
    await user.click(submit)
    expect(apiMocks.createArticleShare).toHaveBeenCalledWith(42, new Date('2026-09-10T09:30').toISOString())
  })

  it('lists all statuses and only allows actions on active non-legacy rows', async () => {
    apiMocks.listArticleShares.mockResolvedValue([
      activeShare('active'), expiredShare('expired'), revokedShare('revoked'), legacyShare('legacy'),
    ])
    const user = userEvent.setup()
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    renderShareDialog()

    expect(await screen.findByText('已过期')).toBeTruthy()
    expect(screen.getByText('已撤销')).toBeTruthy()
    expect(screen.getByText('旧版链接')).toBeTruthy()
    expect(screen.getAllByRole('button', { name: '复制链接' })).toHaveLength(1)
    expect(screen.getAllByRole('button', { name: '分享到 X' })).toHaveLength(1)
    expect(screen.getAllByRole('button', { name: '撤销' })).toHaveLength(1)

    await user.click(screen.getByRole('button', { name: '分享到 X' }))
    const intent = new URL(String(open.mock.calls[0][0]))
    expect(intent.origin + intent.pathname).toBe('https://twitter.com/intent/tweet')
    expect(intent.searchParams.get('text')).toBe('A useful article')
    expect(intent.searchParams.get('url')).toBe('https://rss.example/share/signed-active')
    expect(open).toHaveBeenCalledWith(expect.any(String), '_blank', 'noopener,noreferrer')
  })

  it('revokes only one row and prevents duplicate requests while it is pending', async () => {
    const pending = deferred<ArticleShareListItem>()
    apiMocks.listArticleShares.mockResolvedValue([activeShare('a'), activeShare('b')])
    apiMocks.revokeArticleShare.mockReturnValue(pending.promise)
    const user = userEvent.setup()
    renderShareDialog()

    const firstRow = (await screen.findByText('a')).closest('li')!
    const revoke = within(firstRow).getByRole('button', { name: '撤销' })
    await user.dblClick(revoke)
    expect(apiMocks.revokeArticleShare).toHaveBeenCalledTimes(1)
    expect(apiMocks.revokeArticleShare).toHaveBeenCalledWith(42, 'a')
    pending.resolve(revokedShare('a'))
    expect(await within(firstRow).findByText('已撤销')).toBeTruthy()
    expect(screen.getByText('b')).toBeTruthy()
    expect(screen.getAllByRole('button', { name: '撤销' })).toHaveLength(1)
  })

  it('keeps the dialog open for 409 and reports create, revoke, and clipboard failures', async () => {
    apiMocks.createArticleShare.mockRejectedValueOnce({ response: { status: 409 } })
    const user = userEvent.setup()
    const firstView = renderShareDialog()
    await user.click(await screen.findByRole('button', { name: '创建新链接' }))
    expect(await screen.findByText('文章正文尚未准备完成')).toBeTruthy()
    expect(screen.getByRole('dialog')).toBeTruthy()

    apiMocks.createArticleShare.mockRejectedValueOnce({ response: { data: { error: '创建被拒绝' } } })
    await user.click(screen.getByRole('button', { name: '创建新链接' }))
    expect(await screen.findByText('创建被拒绝')).toBeTruthy()
    firstView.unmount()

    apiMocks.listArticleShares.mockResolvedValueOnce([activeShare('copy'), activeShare('revoke')])
    const view = renderShareDialog()
    const rows = await screen.findAllByRole('listitem')
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValueOnce(new Error('denied'))
    await user.click(within(rows[0]).getByRole('button', { name: '复制链接' }))
    expect(await screen.findByText('复制失败，请手动复制链接')).toBeTruthy()
    apiMocks.revokeArticleShare.mockRejectedValueOnce({ response: { data: { error: '撤销被拒绝' } } })
    await user.click(within(rows[1]).getByRole('button', { name: '撤销' }))
    expect(await screen.findByText('撤销被拒绝')).toBeTruthy()
    view.unmount()
  })

  it('preserves Xiaohongshu and Markdown callbacks and exposes accessible close controls', async () => {
    const user = userEvent.setup()
    const { props } = renderShareDialog()
    const dialog = screen.getByRole('dialog', { name: '管理分享链接' })
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    await user.click(screen.getByRole('button', { name: '小红书复制' }))
    await user.click(screen.getByRole('button', { name: '导出 Markdown' }))
    await user.click(screen.getByRole('button', { name: '关闭' }))
    expect(props.onCopyXiaohongshu).toHaveBeenCalledTimes(1)
    expect(props.onExportMarkdown).toHaveBeenCalledTimes(1)
    expect(props.onClose).toHaveBeenCalledTimes(1)
  })
})
