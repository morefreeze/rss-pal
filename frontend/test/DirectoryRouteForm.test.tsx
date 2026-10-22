import { fireEvent, render, screen, cleanup } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import DirectoryRouteForm from '../src/components/DirectoryRouteForm'
import type { DirectoryPlatform, DirectoryRoute } from '../src/api/sourceDirectory'
afterEach(cleanup)
const route: DirectoryRoute = { id: 'demo/user', name: '用户动态', path: '/user/:uid/:type?', categories: [], description: '<img src=x onerror=alert(1)>', example: '/demo/user/123/posts', parameters: { uid: { description: '用户 ID' }, type: { description: '内容类型', default: 'posts', options: [{ label: '动态', value: 'posts' }] } }, requires_config: true, config_names: ['DEMO_COOKIE'], anti_crawler: true, top_feeds: [{ title: '公开订阅', url: 'rsshub://demo/user/456' }, { title: '不安全订阅', url: 'http://localhost/feed' }] }
const platform: DirectoryPlatform = { id: 'demo', name: '演示平台', url: '', categories: [], heat: 0, routes: [route] }
describe('route configuration', () => {
 it('renders plain descriptions, defaults and configuration flags without previewing automatically', () => {
  const onPreview = vi.fn()
  const { container } = render(<DirectoryRouteForm platform={platform} route={route} onPreview={onPreview} onBack={() => {}} />)
  expect(container.querySelector('img')).toBeNull()
  expect(screen.getByText(route.description)).toBeTruthy()
  expect(screen.getByText(/DEMO_COOKIE/)).toBeTruthy()
  expect((screen.getByLabelText(/type（可选）/) as HTMLSelectElement).value).toBe('posts')
  expect(onPreview).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '预览订阅' }))
  expect(screen.getByRole('alert').textContent).toContain('uid')
  fireEvent.change(screen.getByLabelText(/uid（必填）/), { target: { value: '中文/a' } })
  fireEvent.click(screen.getByRole('button', { name: '预览订阅' }))
  expect(onPreview).toHaveBeenCalledWith('https://rsshub.app/demo/user/%E4%B8%AD%E6%96%87%2Fa/posts')
 })
 it('fills an example only by explicit action and previews safe top feeds explicitly', () => {
  const onPreview = vi.fn()
  render(<DirectoryRouteForm platform={platform} route={route} onPreview={onPreview} onBack={() => {}} />)
  expect((screen.getByLabelText(/uid（必填）/) as HTMLInputElement).value).toBe('')
  fireEvent.click(screen.getByRole('button', { name: '填入示例路径' }))
  expect(onPreview).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '预览订阅' }))
  expect(onPreview).toHaveBeenLastCalledWith('https://rsshub.app/demo/user/123/posts')
  expect(screen.queryByRole('button', { name: /不安全订阅/ })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '预览示例：公开订阅' }))
  expect(onPreview).toHaveBeenLastCalledWith('https://rsshub.app/demo/user/456')
 })
 it('blocks credential routes in every mode and example flow', () => {
  const onPreview = vi.fn()
  render(<DirectoryRouteForm platform={platform} route={{ ...route, path: '/:token', example: '/demo/secret', parameters: { token: { description: 'Private token' } } }} onPreview={onPreview} onBack={() => {}} />)
  expect(screen.queryByLabelText('完整 RSSHub 路径')).toBeNull()
  expect(screen.queryByRole('button', { name: '填入示例路径' })).toBeNull()
  expect(screen.queryByRole('button', { name: '预览示例：公开订阅' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '预览订阅' }))
  expect(onPreview).not.toHaveBeenCalled()
  expect(screen.queryByRole('checkbox')).toBeNull()
 })
 it('retains complex routes with editable paths and resets values when choosing an alias', () => {
  const onPreview = vi.fn()
  render(<DirectoryRouteForm platform={platform} route={{ ...route, path: '/product/*', paths: ['/product/*', '/user/:uid'], example: '/demo/product/123' }} onPreview={onPreview} onBack={() => {}} />)
  fireEvent.click(screen.getByRole('button', { name: '预览订阅' }))
  expect(onPreview).toHaveBeenCalledWith('https://rsshub.app/demo/product/123')
  fireEvent.change(screen.getByLabelText('路径模式'), { target: { value: '/user/:uid' } })
  expect((screen.getByLabelText(/uid（必填）/) as HTMLInputElement).value).toBe('')
 })
})
