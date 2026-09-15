import { beforeEach, describe, expect, it, vi } from 'vitest'
const mocked = vi.hoisted(() => ({post: vi.fn(), rejected: undefined as undefined | ((err: unknown) => Promise<unknown>)}))
vi.mock('axios', () => ({default: {
 create: () => ({interceptors:{request:{use:vi.fn()},response:{use:(_success: unknown, rejected: typeof mocked.rejected) => {mocked.rejected=rejected}}}}),
 post: mocked.post,
}}))
vi.mock('../src/components/CollapsibleFab', () => ({clearAllFabCollapsed: vi.fn()}))
import '../src/api/client'
beforeEach(() => {vi.clearAllMocks();localStorage.clear();localStorage.setItem('token','expired-access');localStorage.setItem('refresh_token','valid-refresh')})
describe('remembered sessions under temporary authentication failure', () => {
 it.each([429,503,undefined])('preserves credentials for refresh status %s', async status => {
  const transient={response:status ? {status}:undefined}
  mocked.post.mockRejectedValue(transient)
  await expect(mocked.rejected!({response:{status:401},config:{url:'/articles',method:'get',headers:{}}})).rejects.toBe(transient)
  expect(localStorage.getItem('refresh_token')).toBe('valid-refresh')
  expect(localStorage.getItem('token')).toBe('expired-access')
 })
})
