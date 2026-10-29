import { afterEach, expect, it, vi } from 'vitest'
import { createQQOAuthPopup, completeQQOAuthPopup } from '../src/utils/qqOAuthPopup'
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); sessionStorage.clear() })
function fixture() {
 const channel = {onmessage:null as any, postMessage:vi.fn(),close:vi.fn()}
 vi.stubGlobal('BroadcastChannel',vi.fn(function(){return channel}))
 const popup = {opener:window,closed:false,sessionStorage:{setItem:vi.fn()},location:{replace:vi.fn()},close:vi.fn()}
 const open = vi.spyOn(window,'open').mockReturnValue(popup as unknown as Window)
 return {channel,popup,open}
}
it('opens synchronously and transfers only the OAuth flow before navigating', () => {
 const {popup,open,channel}=fixture();const complete=vi.fn()
 const controller=createQQOAuthPopup(complete)
 expect(open).toHaveBeenCalledWith('about:blank','_blank',expect.stringContaining('width=450,height=320'))
 sessionStorage.setItem('rsspal:account-oauth-flow',JSON.stringify({owner:null,binding:false,createdAt:Date.now(),returnTo:'/feeds'}))
 controller.navigate('https://graph.qq.com/oauth2.0/authorize?client_id=fixture')
 expect(popup.opener).toBeNull()
 const transferred=JSON.parse(popup.sessionStorage.setItem.mock.calls[0][1])
 expect(transferred.returnTo).toBe('/feeds');expect(transferred.popupChannel).toMatch(/^rsspal-qq-/)
 expect(popup.location.replace).toHaveBeenCalledWith(expect.stringContaining('https://graph.qq.com/'))
 channel.onmessage({data:'unexpected'});expect(complete).not.toHaveBeenCalled()
 channel.onmessage({data:'complete'});expect(complete).toHaveBeenCalledOnce();expect(channel.close).toHaveBeenCalledOnce()
 controller.dispose()
})
it('reports a blocked popup before attempting authorization', () => {
 fixture();vi.mocked(window.open).mockReturnValue(null)
 expect(()=>createQQOAuthPopup(vi.fn())).toThrow('请允许弹出窗口后重试 QQ 登录')
})
it('rejects foreign destinations and closes abandoned windows', () => {
 const {popup}=fixture();const controller=createQQOAuthPopup(vi.fn())
 expect(()=>controller.navigate('https://example.com/')).toThrow()
 controller.dispose();expect(popup.close).toHaveBeenCalled()
})
it('notifies the original page only for a popup flow', () => {
 const {channel}=fixture();const close=vi.spyOn(window,'close').mockImplementation(()=>{})
 completeQQOAuthPopup(undefined)
 expect(channel.postMessage).not.toHaveBeenCalled()
 completeQQOAuthPopup('rsspal-qq-fixture')
 expect(channel.postMessage).toHaveBeenCalledWith('complete');expect(close).toHaveBeenCalled()
})

it('does not turn a successful login into an error when notification is unavailable', () => {
 vi.stubGlobal('BroadcastChannel',vi.fn(function(){throw Error('unavailable')}))
 expect(()=>completeQQOAuthPopup('rsspal-qq-fixture')).not.toThrow()
})
