import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ExploreSourceDrawer from '../src/components/ExploreSourceDrawer'
import type { ExploreSource } from '../src/api/client'

const api = vi.hoisted(() => ({
  getExploreSources: vi.fn(),
  subscribeExploreSource: vi.fn(),
  subscribeExploreSources: vi.fn(),
}))

const breakpoint = vi.hoisted(() => ({ value: 'desktop' as 'desktop' | 'tablet' | 'phone' }))

vi.mock('../src/api/client', async importOriginal => ({
  ...await importOriginal<typeof import('../src/api/client')>(),
  ...api,
}))

vi.mock('../src/hooks/useBreakpoint', () => ({ useBreakpoint: () => breakpoint.value }))

const sources: ExploreSource[] = [
  {
    id: 7,
    title: 'Pragmatic Engineer',
    url: 'https://example.test/feed',
    rank: 1,
    topic: '工程',
    reason: '与你的工程订阅高度相关',
    health_score: 0.96,
    validation_status: 'valid',
    recent_article_count: 5,
    selected: false,
    is_hidden: false,
    is_subscribed: false,
  },
  {
    id: 8,
    title: 'Design Notes',
    url: 'https://design.test/feed',
    rank: 2,
    topic: '设计',
    reason: '补充相邻方向',
    validation_status: 'valid',
    recent_article_count: 3,
    selected: true,
    is_hidden: false,
    is_subscribed: true,
  },
]

describe('ExploreSourceDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    breakpoint.value = 'desktop'
    api.getExploreSources.mockResolvedValue(sources)
    api.subscribeExploreSource.mockResolvedValue({ feed_id: 17, created: true, copied_articles: 5 })
    api.subscribeExploreSources.mockResolvedValue({ results: [{ source_id: 7, feed_id: 17, created: true, copied_articles: 5 }] })
    document.body.style.overflow = ''
  })

  it('is closed by default and opens as a desktop right drawer from its count handle', async () => {
    render(<ExploreSourceDrawer />)
    const handle = await screen.findByRole('button', { name: '查看 2 个候选源' })
    expect(screen.queryByRole('dialog')).toBeNull()
    fireEvent.click(handle)
    const dialog = screen.getByRole('dialog', { name: '候选订阅源' })
    expect(dialog.getAttribute('data-placement')).toBe('right')
    expect(document.body.style.overflow).toBe('hidden')
    expect(screen.queryByText('订阅全部')).toBeNull()
  })

  it('opens as a mobile bottom sheet and closes on outside click or Escape', async () => {
    breakpoint.value = 'phone'
    render(<ExploreSourceDrawer />)
    const handle = await screen.findByRole('button', { name: '查看 2 个候选源' })
    fireEvent.click(handle)
    expect(screen.getByRole('dialog').getAttribute('data-placement')).toBe('bottom')
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '关闭候选订阅源' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(document.activeElement).toBe(handle)

    fireEvent.click(screen.getByRole('button', { name: '查看 2 个候选源' }))
    fireEvent.pointerDown(screen.getByTestId('explore-drawer-backdrop'))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('subscribes one source, marks it subscribed, and closes after success', async () => {
    render(<ExploreSourceDrawer />)
    fireEvent.click(await screen.findByRole('button', { name: '查看 2 个候选源' }))
    fireEvent.click(screen.getByRole('button', { name: '订阅 Pragmatic Engineer' }))
    await waitFor(() => expect(api.subscribeExploreSource).toHaveBeenCalledWith(7))
    expect(screen.queryByRole('dialog')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: '查看 2 个候选源' }))
    expect(screen.getAllByRole('button', { name: '已订阅' })).toHaveLength(2)
  })

  it('keeps the drawer open and allows retry after a single subscription error', async () => {
    api.subscribeExploreSource
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValueOnce({ feed_id: 17, created: true, copied_articles: 5 })
    render(<ExploreSourceDrawer />)
    fireEvent.click(await screen.findByRole('button', { name: '查看 2 个候选源' }))
    fireEvent.click(screen.getByRole('button', { name: '订阅 Pragmatic Engineer' }))
    expect((await screen.findByRole('alert')).textContent).toContain('订阅失败，请重试')
    expect(screen.getByRole('dialog')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '订阅 Pragmatic Engineer' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('selects candidates and batch subscribes only the checked unsubscribed sources', async () => {
    render(<ExploreSourceDrawer />)
    fireEvent.click(await screen.findByRole('button', { name: '查看 2 个候选源' }))
    const checkbox = screen.getByRole('checkbox', { name: '选择 Pragmatic Engineer' })
    fireEvent.click(checkbox)
    const batch = screen.getByRole('button', { name: '订阅已选 1 个' })
    fireEvent.click(batch)
    await waitFor(() => expect(api.subscribeExploreSources).toHaveBeenCalledWith([7]))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('recovers selection after a failed batch subscription', async () => {
    api.subscribeExploreSources.mockRejectedValue(new Error('network'))
    render(<ExploreSourceDrawer />)
    fireEvent.click(await screen.findByRole('button', { name: '查看 2 个候选源' }))
    fireEvent.click(screen.getByRole('checkbox', { name: '选择 Pragmatic Engineer' }))
    fireEvent.click(screen.getByRole('button', { name: '订阅已选 1 个' }))
    expect((await screen.findByRole('alert')).textContent).toContain('批量订阅失败，请重试')
    expect((screen.getByRole('checkbox', { name: '选择 Pragmatic Engineer' }) as HTMLInputElement).checked).toBe(true)
    expect(screen.getByRole('dialog')).toBeTruthy()
  })

  it('shows an accessible load error and retries instead of presenting a false empty state', async () => {
    api.getExploreSources
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(sources)
    render(<ExploreSourceDrawer />)
    const handle = await screen.findByRole('button', { name: /查看候选源/ })
    fireEvent.click(handle)

    expect((await screen.findByRole('alert')).textContent).toContain('候选源加载失败')
    expect(screen.queryByText('当前没有可管理的候选源')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '重试加载候选源' }))

    expect(await screen.findByText('Pragmatic Engineer')).toBeTruthy()
    expect(api.getExploreSources).toHaveBeenCalledTimes(2)
  })

  it('disables pending, invalid, broken, and merged sources with explicit health states', async () => {
    api.getExploreSources.mockResolvedValue([
      { ...sources[0], id: 10, title: 'Pending Source', validation_status: 'pending', selected: true },
      { ...sources[0], id: 11, title: 'Invalid Source', validation_status: 'invalid', selected: true },
      { ...sources[0], id: 12, title: 'Broken Source', is_broken: true, selected: true },
      { ...sources[0], id: 13, title: 'Merged Source', merged_into_source_id: 7, selected: true },
      { ...sources[0], id: 14, title: 'Healthy Source', selected: true },
    ])
    render(<ExploreSourceDrawer />)
    fireEvent.click(await screen.findByRole('button', { name: '查看 5 个候选源' }))

    expect(screen.getByText('待校验')).toBeTruthy()
    expect(screen.getByText('无效')).toBeTruthy()
    expect(screen.getByText('已失效')).toBeTruthy()
    expect(screen.getByText('已合并')).toBeTruthy()
    for (const title of ['Pending Source', 'Invalid Source', 'Broken Source', 'Merged Source']) {
      expect((screen.getByRole('checkbox', { name: `选择 ${title}` }) as HTMLInputElement).disabled).toBe(true)
      expect((screen.getByRole('button', { name: `订阅 ${title}` }) as HTMLButtonElement).disabled).toBe(true)
    }
    expect((screen.getByRole('checkbox', { name: '选择 Healthy Source' }) as HTMLInputElement).checked).toBe(true)
    expect((screen.getByRole('button', { name: '订阅已选 1 个' }) as HTMLButtonElement).disabled).toBe(false)
  })
})
