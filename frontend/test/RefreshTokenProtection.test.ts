import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
const mocked = vi.hoisted(() => ({post: vi.fn(), rejected: undefined as undefined | ((err: unknown) => Promise<unknown>)}))
vi.mock('axios', () => ({default: {
 create: () => ({interceptors:{request:{use:vi.fn()},response:{use:(_success: unknown, rejected: typeof mocked.rejected) => {mocked.rejected=rejected}}}}),
 post: mocked.post,
}}))
vi.mock('../src/components/CollapsibleFab', () => ({clearAllFabCollapsed: vi.fn()}))
import '../src/api/client'
beforeEach(() => {vi.clearAllMocks();localStorage.clear();localStorage.setItem('token','expired-access');localStorage.setItem('refresh_token','valid-refresh')})
afterEach(() => vi.unstubAllGlobals())
describe('remembered sessions under temporary authentication failure', () => {
 it('retains a shared capture URL when credentials expire definitively', async () => {
  const source=`https://example.com/${'文'.repeat(260)}?a=1&b=2`
  const location={pathname:'/capture',search:`?url=${encodeURIComponent(source)}`,href:''}
  vi.stubGlobal('window',{location})
  const denied={response:{status:401}}
  mocked.post.mockRejectedValue(denied)
  await expect(mocked.rejected!({response:{status:401},config:{url:'/feeds/oneoff_link_set',method:'post',headers:{}}})).rejects.toMatchObject({response:{status:401}})
  expect(location.href).toBe(`/login?intent=capture&source=${encodeURIComponent(source)}`)
  expect(localStorage.getItem('token')).toBeNull()
 })

 it('does not preserve ambiguous shared input after credential rejection', async () => {
  const location={pathname:'/capture',search:'?url=https://example.com&url=https://other.example',href:''}
  vi.stubGlobal('window',{location})
  mocked.post.mockRejectedValue({response:{status:401}})
  await expect(mocked.rejected!({response:{status:401},config:{url:'/feeds/oneoff_link_set',method:'post',headers:{}}})).rejects.toMatchObject({response:{status:401}})
  expect(location.href).toBe('/login')
 })
 it.each([429,503,undefined])('preserves credentials for refresh status %s', async status => {
  const transient={response:status ? {status}:undefined}
  mocked.post.mockRejectedValue(transient)
  await expect(mocked.rejected!({response:{status:401},config:{url:'/articles',method:'get',headers:{}}})).rejects.toBe(transient)
  expect(localStorage.getItem('refresh_token')).toBe('valid-refresh')
  expect(localStorage.getItem('token')).toBe('expired-access')
 })
})
