import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import SourceDirectory from '../src/components/SourceDirectory'
import type { DirectoryRoute, FullSourceDirectory } from '../src/api/sourceDirectory'

afterEach(cleanup)
const route = (id: string, name: string, categories: string[], path: string): DirectoryRoute => ({ id, name, categories, path, paths: [path], description: '', example: `/github${path}`, parameters: {}, requires_config: false, config_names: [], anti_crawler: false, top_feeds: [] })
const directory: FullSourceDirectory = {
  source_url: 'https://docs.rsshub.app/routes.json', fetched_at: '2026-10-22T00:00:00Z', stale: false, platform_count: 2, route_count: 3,
  platforms: [
    { id: 'github', name: 'GitHub', url: 'https://github.com', heat: 100, categories: ['programming','popular'], routes: [route('github:trending','Trending',['programming','popular'],'/trending'),route('github:releases','版本更新',['program-update'],'/release/:user/:repo')] },
    { id: 'design', name: 'Design Site', url: 'https://design.example', heat: 10, categories: ['design'], routes: [route('design:news','设计动态',['design'],'/news')] },
  ],
}
it('shows full platform and route counts and exposes every category route rather than only curated feeds', () => {
  render(<SourceDirectory items={[]} directory={directory} onPreview={vi.fn()} />)
  fireEvent.click(screen.getByRole('button', { name: /^编程.*1 个平台.*1 个订阅项/ }))
  fireEvent.click(screen.getByRole('button', { name: '查看 GitHub 的 1 个订阅项' }))
  expect(screen.getByRole('button', { name: '配置 Trending' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: '配置 版本更新' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '返回分类目录' }))
  fireEvent.click(screen.getByRole('button', { name: /^程序更新.*1 个平台.*1 个订阅项/ }))
  fireEvent.click(screen.getByRole('button', { name: '查看 GitHub 的 1 个订阅项' }))
  expect(screen.getByRole('button', { name: '配置 版本更新' })).toBeTruthy()
})
it('searches beyond curated sources and paginates without losing routes', () => {
  const many = { ...directory, route_count: 61, platforms: [{ ...directory.platforms[0], routes: Array.from({length:61},(_,i)=>route(`item${i}`,`订阅项 ${i}`,['programming'],`/item/${i}`)) }] }
  render(<SourceDirectory items={[]} directory={many} onPreview={vi.fn()} />)
  fireEvent.change(screen.getByRole('searchbox', { name: '搜索推荐源' }), { target: { value: '订阅项 60' } })
  fireEvent.click(screen.getByRole('button', { name: '查看 GitHub 的 1 个订阅项' }))
  fireEvent.click(screen.getByRole('button', { name: '配置 订阅项 60' }))
  expect(screen.getByRole('button', { name: /预览/ })).toBeTruthy()
})
it('retains curated recommendations alongside the complete catalog and retries errors visibly', () => {
  const retry = vi.fn()
  render(<SourceDirectory items={[]} directoryError onReloadDirectory={retry} onPreview={vi.fn()} />)
  expect(screen.getByText('完整目录加载失败')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '重试完整目录' }))
  expect(retry).toHaveBeenCalledOnce()
})
it('keeps every route reachable after multiple route pages', () => {
  const many = { ...directory, route_count: 61, platforms: [{ ...directory.platforms[0], routes: Array.from({length:61},(_,i)=>route(`item${i}`,`路由 ${i}`,['programming'],`/item/${i}`)) }] }
  render(<SourceDirectory items={[]} directory={many} onPreview={vi.fn()} />)
  fireEvent.click(screen.getByRole('button', { name: /^编程.*1 个平台.*61 个订阅项/ }))
  fireEvent.click(screen.getByRole('button', { name: '查看 GitHub 的 61 个订阅项' }))
  expect(screen.queryByRole('button', { name: '配置 路由 60' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /加载更多订阅项/ }))
  fireEvent.click(screen.getByRole('button', { name: /加载更多订阅项/ }))
  expect(screen.getAllByRole('button', { name: /^配置 路由/ })).toHaveLength(61)
  expect(screen.getByRole('button', { name: '配置 路由 60' })).toBeTruthy()
})
it('keeps every platform reachable after multiple platform pages', () => {
  const many = { ...directory, platform_count: 61, route_count: 61, platforms: Array.from({length:61},(_,i)=>({ ...directory.platforms[0], id:`platform${i}`,name:`平台 ${i}`,routes:[route(`p${i}`,'动态',['design'],'/news')] })) }
  render(<SourceDirectory items={[]} directory={many} onPreview={vi.fn()} />)
  fireEvent.click(screen.getByRole('button', { name: /^设计.*61 个平台.*61 个订阅项/ }))
  expect(screen.queryByRole('button', { name: '查看 平台 60 的 1 个订阅项' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /加载更多平台/ }))
  fireEvent.click(screen.getByRole('button', { name: /加载更多平台/ }))
  expect(screen.getAllByRole('button', { name: /^查看 平台/ })).toHaveLength(61)
})
