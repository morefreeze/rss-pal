import { beforeEach, expect, it } from 'vitest'
import { logout } from '../src/api/client'
import { persistAccountSession } from '../src/api/accountAuth'
beforeEach(() => { localStorage.clear(); sessionStorage.clear() })
it('clears per-login suppression on logout while retaining the one-time nickname record', () => {
 localStorage.setItem('token','test'); localStorage.setItem('user','malformed')
 sessionStorage.setItem('rsspal:nickname-skipped:19','1')
 sessionStorage.setItem('rsspal:password-skipped:19','1')
 localStorage.setItem('rsspal:nickname-prompted:19','1')
 expect(() => logout()).not.toThrow()
 expect(sessionStorage.getItem('rsspal:nickname-skipped:19')).toBeNull()
 expect(sessionStorage.getItem('rsspal:password-skipped:19')).toBeNull()
 expect(localStorage.getItem('rsspal:nickname-prompted:19')).toBe('1')
 expect(localStorage.getItem('token')).toBeNull()
})
it('resets password guidance on a new login without repeating nickname guidance or affecting another account', () => {
 sessionStorage.setItem('rsspal:nickname-skipped:19','1');sessionStorage.setItem('rsspal:nickname-skipped:20','1')
 sessionStorage.setItem('rsspal:password-skipped:19','1');sessionStorage.setItem('rsspal:password-skipped:20','1')
 localStorage.setItem('rsspal:nickname-prompted:19','1')
 persistAccountSession({token:'test',user:{id:19,username:'reader',is_admin:false}})
 expect(sessionStorage.getItem('rsspal:nickname-skipped:19')).toBeNull()
 expect(sessionStorage.getItem('rsspal:nickname-skipped:20')).toBe('1')
 expect(sessionStorage.getItem('rsspal:password-skipped:19')).toBeNull()
 expect(sessionStorage.getItem('rsspal:password-skipped:20')).toBe('1')
 expect(localStorage.getItem('rsspal:nickname-prompted:19')).toBe('1')
})
