import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ShortcutConnection from '../src/components/ShortcutConnection'

const api = vi.hoisted(() => ({ getShortcutConnections: vi.fn(), pairShortcut: vi.fn(), revokeShortcutConnections: vi.fn() }))
vi.mock('../src/api/client', () => api)

beforeEach(() => {
 vi.clearAllMocks()
 api.getShortcutConnections.mockResolvedValue(0)
 api.pairShortcut.mockResolvedValue({ code: 'a'.repeat(64), expires_at: new Date(Date.now()+300000).toISOString() })
 api.revokeShortcutConnections.mockResolvedValue(undefined)
})
describe('shortcut account connection', () => {
 it('provides a native connection link containing only a one-time code', async () => {
  render(<ShortcutConnection />)
  fireEvent.click(await screen.findByRole('button',{name:'连接当前账号'}))
  const link = await screen.findByRole('link',{name:'打开快捷指令完成连接'})
  expect(link.getAttribute('href')).not.toContain('+')
  expect(link.getAttribute('href')).toContain('%20RSS%20Pal')
  const url = new URL(link.getAttribute('href')!)
  expect(url.protocol).toBe('shortcuts:')
  expect(url.searchParams.get('name')).toBe('加入 RSS Pal')
  expect(url.searchParams.get('text')).toBe('rsspal-pair:'+ 'a'.repeat(64))
  expect(url.href).not.toContain('token=')
  expect(screen.getByText(/不需要在 Chrome 里另外登录/)).toBeTruthy()
 })
 it('revokes active credentials and clears a pending connection link', async () => {
  api.getShortcutConnections.mockResolvedValue(2)
  render(<ShortcutConnection />)
  await screen.findByText('已连接 2 个快捷指令')
  fireEvent.click(screen.getByRole('button',{name:'连接当前账号'}))
  await screen.findByRole('link',{name:'打开快捷指令完成连接'})
  fireEvent.click(screen.getByRole('button',{name:'断开所有快捷指令'}))
  await waitFor(()=>expect(api.revokeShortcutConnections).toHaveBeenCalledOnce())
  await waitFor(()=>expect(screen.queryByRole('link',{name:'打开快捷指令完成连接'})).toBeNull())
  expect(await screen.findByText('已断开连接，旧快捷指令将无法提交抓取。')).toBeTruthy()
 })
 it('removes an expired one-time connection link', async () => {
  vi.useFakeTimers()
  try {
   render(<ShortcutConnection />)
   await act(async () => { await Promise.resolve() })
   await act(async () => { fireEvent.click(screen.getByRole('button', { name: '连接当前账号' })) })
   expect(screen.getByRole('link', { name: '打开快捷指令完成连接' })).toBeTruthy()
   act(() => { vi.advanceTimersByTime(300001) })
   expect(screen.queryByRole('link', { name: '打开快捷指令完成连接' })).toBeNull()
   expect(screen.getByText('连接已过期，请重新点“连接当前账号”。')).toBeTruthy()
  } finally { vi.useRealTimers() }
 })
 it('shows failures instead of pretending connection succeeded', async () => {
  api.pairShortcut.mockRejectedValue(new Error('offline'))
  render(<ShortcutConnection />)
  fireEvent.click(await screen.findByRole('button',{name:'连接当前账号'}))
  expect(await screen.findByRole('alert')).toHaveProperty('textContent','创建连接失败，请重试。')
  expect(screen.queryByRole('link',{name:'打开快捷指令完成连接'})).toBeNull()
 })
})
