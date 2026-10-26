import { beforeEach, expect, it } from 'vitest'
import { logout } from '../src/api/client'
import { persistAccountSession } from '../src/api/accountAuth'
beforeEach(() => { localStorage.clear(); sessionStorage.clear() })
it('clears skipped prompts on logout even if the user cache is malformed', () => {
 localStorage.setItem('token','test'); localStorage.setItem('user','malformed')
 sessionStorage.setItem('rsspal:nickname-skipped:19','1')
 expect(() => logout()).not.toThrow()
 expect(sessionStorage.getItem('rsspal:nickname-skipped:19')).toBeNull()
 expect(localStorage.getItem('token')).toBeNull()
})
it('offers setup again after a new email login without affecting another account', () => {
 sessionStorage.setItem('rsspal:nickname-skipped:19','1');sessionStorage.setItem('rsspal:nickname-skipped:20','1')
 persistAccountSession({token:'test',user:{id:19,username:'reader',is_admin:false}})
 expect(sessionStorage.getItem('rsspal:nickname-skipped:19')).toBeNull()
 expect(sessionStorage.getItem('rsspal:nickname-skipped:20')).toBe('1')
})
