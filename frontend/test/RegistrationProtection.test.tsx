import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RegisterPage from '../src/pages/RegisterPage'
import LoginPage from '../src/pages/LoginPage'

const mocks = vi.hoisted(() => ({ register: vi.fn(), login: vi.fn(), config: vi.fn(), post: vi.fn(), widget: vi.fn(), remove: vi.fn(), tencent: vi.fn() }))
vi.mock('../src/utils/tencentCaptcha',()=>({verifyTencentCaptcha:mocks.tencent}))
vi.mock('../src/api/client', () => ({register: mocks.register, login: mocks.login, getRegistrationConfig: mocks.config, api: {post: mocks.post}}))

beforeEach(() => {
 vi.clearAllMocks()
 mocks.config.mockResolvedValue({available: true, site_key: 'test-site-key'})
 mocks.post.mockRejectedValue(new Error('admin exists'))
 mocks.widget.mockReturnValue('widget-1')
 window.turnstile = {render: mocks.widget, remove: mocks.remove}
})

function registration() { return render(<MemoryRouter><RegisterPage onLogin={vi.fn()} /></MemoryRouter>) }
function fillForm() {
 fireEvent.change(screen.getByPlaceholderText('邀请码'), {target:{value:'RSS-INVITE'}})
 fireEvent.change(screen.getByPlaceholderText('用户名'), {target:{value:'alice'}})
 fireEvent.change(screen.getByPlaceholderText('密码（至少 6 位）'), {target:{value:'test-password'}})
}

describe('registration protection', () => {
 it.each([
  ['', 'RSS-INVITE', 'test-password'],
  ['alice', '', 'test-password'],
  ['a'.repeat(65), 'RSS-INVITE', 'test-password'],
  ['alice','x'.repeat(33),'test-password'],
  ['alice','RSS-INVITE','密'.repeat(25)],
 ])('does not spend a Tencent verification on invalid registration fields',async(username,code,password)=>{
  mocks.config.mockResolvedValue({available:true,provider:'tencent',site_key:'123'})
  registration()
  fireEvent.change(screen.getByPlaceholderText('用户名'),{target:{value:username}})
  fireEvent.change(screen.getByPlaceholderText('邀请码'),{target:{value:code}})
  fireEvent.change(screen.getByPlaceholderText('密码（至少 6 位）'),{target:{value:password}})
  await waitFor(()=>expect((screen.getByRole('button',{name:'注册'}) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole('button',{name:'注册'}))
  expect(mocks.tencent).not.toHaveBeenCalled()
  expect(mocks.register).not.toHaveBeenCalled()
 })
 it('launches Tencent on submit and sends proof only after verification',async()=>{
  mocks.config.mockResolvedValue({available:true,provider:'tencent',site_key:'123'})
  let verify:any
  mocks.tencent.mockImplementation(()=>new Promise(resolve=>{verify=resolve}))
  mocks.register.mockRejectedValue({response:{status:429,data:{error:'操作过于频繁'}}})
  registration();fillForm()
  await waitFor(()=>expect((screen.getByRole('button',{name:'注册'}) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole('button',{name:'注册'}))
  expect(mocks.tencent).toHaveBeenCalledWith('123',expect.any(AbortSignal))
  expect(mocks.register).not.toHaveBeenCalled()
  await act(async()=>verify('{"ticket":"ticket","randstr":"rand"}'))
  await waitFor(()=>expect(mocks.register).toHaveBeenCalledWith('alice','test-password','RSS-INVITE','{"ticket":"ticket","randstr":"rand"}'))
  expect(mocks.widget).not.toHaveBeenCalled()
 })
 it('does not register when Tencent fails and permits a retry',async()=>{
  mocks.config.mockResolvedValue({available:true,provider:'tencent',site_key:'123'})
  mocks.tencent.mockRejectedValue(new Error('人机验证未完成，请重试'))
  registration();fillForm()
  await waitFor(()=>expect((screen.getByRole('button',{name:'注册'}) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole('button',{name:'注册'}))
  expect(await screen.findByText('人机验证未完成，请重试')).toBeTruthy()
  expect(mocks.register).not.toHaveBeenCalled()
  expect((screen.getByRole('button',{name:'注册'}) as HTMLButtonElement).disabled).toBe(false)
 })
 it('registers through a share without a separate invitation code', async () => {
  mocks.register.mockRejectedValue({response:{status:400,data:{error:'invalid share'}}})
  render(<MemoryRouter initialEntries={['/register?intent=use&share=s%3Ao6XKeCZTDZ22']}><RegisterPage onLogin={vi.fn()} /></MemoryRouter>)
  expect(screen.queryByPlaceholderText('邀请码')).toBeNull()
  expect(screen.getByText('通过分享邀请注册，无需另填邀请码')).toBeTruthy()
  fireEvent.change(screen.getByPlaceholderText('用户名'),{target:{value:'reader'}})
  fireEvent.change(screen.getByPlaceholderText('密码（至少 6 位）'),{target:{value:'test-password'}})
  await waitFor(()=>expect(mocks.widget).toHaveBeenCalled())
  await act(async()=>mocks.widget.mock.calls.at(-1)![1].callback('fresh-proof'))
  fireEvent.click(screen.getByRole('button',{name:'注册'}))
  await waitFor(()=>expect(mocks.register).toHaveBeenCalledWith('reader','test-password','','fresh-proof','s:o6XKeCZTDZ22'))
  expect(screen.getByRole('link',{name:'已有账号？登录'}).getAttribute('href')).toBe('/login?intent=use&share=s%3Ao6XKeCZTDZ22')
 })
 it('requires verification, sends proof and requires a fresh proof after failure', async () => {
  mocks.register.mockRejectedValue({response:{status:429,headers:{'retry-after':'60'},data:{error:'操作过于频繁，请稍后重试'}}})
  registration(); fillForm()
  await waitFor(() => expect(mocks.widget).toHaveBeenCalled())
  expect((screen.getByRole('button',{name:'注册'}) as HTMLButtonElement).disabled).toBe(true)
  await act(async () => mocks.widget.mock.calls.at(-1)![1].callback('fresh-proof'))
  fireEvent.click(screen.getByRole('button',{name:'注册'}))
  await waitFor(() => expect(mocks.register).toHaveBeenCalledWith('alice','test-password','RSS-INVITE','fresh-proof'))
  expect(await screen.findByText(/操作过于频繁/)).toBeTruthy()
  expect((screen.getByRole('button',{name:'注册'}) as HTMLButtonElement).disabled).toBe(true)
  expect((screen.getByPlaceholderText('用户名') as HTMLInputElement).value).toBe('alice')
  expect(mocks.remove).toHaveBeenCalledWith('widget-1')
 })
 it('keeps registration disabled when configuration is unavailable', async () => {
  mocks.config.mockRejectedValue(new Error('offline'))
  registration()
  expect(await screen.findByText(/注册验证暂时不可用/)).toBeTruthy()
  expect((screen.getByRole('button',{name:'注册'}) as HTMLButtonElement).disabled).toBe(true)
  expect(mocks.widget).not.toHaveBeenCalled()
 })
 it('invalidates expired and failed proofs', async () => {
  registration(); await waitFor(() => expect(mocks.widget).toHaveBeenCalled())
  const options=mocks.widget.mock.calls.at(-1)![1]
  await act(async () => options.callback('proof'))
  await act(async () => options['expired-callback']())
  expect((screen.getByRole('button',{name:'注册'}) as HTMLButtonElement).disabled).toBe(true)
  await act(async () => options.callback('new-proof'))
  await act(async () => options['error-callback']())
  expect((screen.getByRole('button',{name:'注册'}) as HTMLButtonElement).disabled).toBe(true)
 })
 it('shows login rate limits without claiming the password is wrong', async () => {
  mocks.login.mockRejectedValue({response:{status:429,headers:{'retry-after':'900'},data:{error:'操作过于频繁，请稍后重试'}}})
  render(<MemoryRouter><LoginPage onLogin={vi.fn()} /></MemoryRouter>)
  fireEvent.change(screen.getByPlaceholderText('用户名'),{target:{value:'alice'}})
  fireEvent.change(screen.getByPlaceholderText('密码'),{target:{value:'test-password'}})
  fireEvent.click(screen.getByRole('button',{name:'登录'}))
  expect(await screen.findByText(/操作过于频繁/)).toBeTruthy()
  expect(screen.queryByText('用户名或密码错误')).toBeNull()
 })
})
