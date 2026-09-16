import {afterEach,describe,it,expect,vi} from 'vitest'
import {verifyTencentCaptcha} from '../src/utils/tencentCaptcha'
afterEach(()=>{ delete window.TencentCaptcha })
describe('Tencent registration proof',()=>{
 it('shows challenge and resolves both ticket fields only on success',async()=>{
  let callback:any
  const show=vi.fn(),destroy=vi.fn()
  window.TencentCaptcha=vi.fn(function(_id:string,cb:any){callback=cb;return {show,destroy}}) as any
  const pending=verifyTencentCaptcha('123',new AbortController().signal)
  await vi.waitFor(()=>expect(show).toHaveBeenCalled())
  callback({ret:0,ticket:'ticket',randstr:'random'})
  expect(JSON.parse(await pending)).toEqual({ticket:'ticket',randstr:'random'})
  expect(destroy).toHaveBeenCalled()
 })
 it.each([{ret:2},{ret:0,ticket:'',randstr:'x'},{ret:0,ticket:'trerror_fake',randstr:'x'}])('rejects cancellation or incomplete proof %j',async(result)=>{
  window.TencentCaptcha=vi.fn(function(_id:string,cb:any){return {show(){cb(result)},destroy(){}}}) as any
  await expect(verifyTencentCaptcha('123',new AbortController().signal)).rejects.toThrow()
 })
 it('aborts without accepting a late callback',async()=>{
  let callback:any
  window.TencentCaptcha=vi.fn(function(_id:string,cb:any){callback=cb;return {show(){},destroy(){}}}) as any
  const controller=new AbortController()
  const pending=verifyTencentCaptcha('123',controller.signal)
  await vi.waitFor(()=>expect(callback).toBeTruthy())
  controller.abort(); callback({ret:0,ticket:'ticket',randstr:'rand'})
  await expect(pending).rejects.toThrow()
 })
})
