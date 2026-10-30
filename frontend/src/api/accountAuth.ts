import type { User } from '../App'
import axios from 'axios'
import { api } from './client'
export type AccountProvider = 'google' | 'apple' | 'github' | 'qq'
export type AuthMethods = {
  public_registration: boolean
  legacy_password: boolean
  email: boolean
  qq: boolean
  qq_review_pending?: boolean
  github: boolean
  google: boolean
  apple: boolean
}
export type AccountSession = {
  token: string
  user: User
  refresh_token?: string
  refresh_token_expires_at?: string
  return_to?: string
}
export type AccountAuthResult =
  AccountSession | { bound: true; return_to?: string }
export type AccountIdentity = {
  display_name?: string
  avatar_url?: string
  provider: 'email' | AccountProvider
  email?: string
  created_at?: string
}
export const publicAuth = axios.create({
  baseURL: '/api',
  timeout: 20000,
  withCredentials: true,
})
export async function getAuthMethods(): Promise<AuthMethods> {
  return (await api.get('/auth/methods')).data
}
export async function getAccountIdentities(): Promise<{
  identities: AccountIdentity[]
}> {
  return (await api.get('/auth/identities')).data
}
export function currentAccountID(): number | null {
  try {
    const id = JSON.parse(localStorage.getItem('user') || 'null')?.id
    return localStorage.getItem('token') && Number.isSafeInteger(id) && id > 0
      ? id
      : null
  } catch {
    return null
  }
}
export function persistAccountSession(v: AccountSession) {
  if (!v?.token || !Number.isSafeInteger(v.user?.id) || v.user.id <= 0)
    throw Error('登录响应无效，请重试')
  sessionStorage.removeItem(`rsspal:nickname-skipped:${v.user.id}`)
  sessionStorage.removeItem(`rsspal:password-skipped:${v.user.id}`)
  localStorage.setItem('token', v.token)
  localStorage.setItem('user', JSON.stringify(v.user))
  for (const key of ['refresh_token', 'refresh_token_expires_at'] as const) {
    if (v[key]) localStorage.setItem(key, v[key]!)
    else localStorage.removeItem(key)
  }
}
export async function requestEmailCode(
  email: string,
  proof: string,
  binding = false,
): Promise<{ challenge_id: string; retry_after: number }> {
  return (
    await (binding ? api : publicAuth).post(
      binding ? '/auth/identities/email/code' : '/auth/email/code',
      { email, captcha_response: proof },
    )
  ).data
}
export async function verifyEmailCode(
  challenge_id: string,
  code: string,
  remember: boolean,
  binding = false,
): Promise<AccountAuthResult> {
  return (
    await (binding ? api : publicAuth).post(
      binding ? '/auth/identities/email/verify' : '/auth/email/verify',
      { challenge_id, code, remember },
    )
  ).data
}
const flowKey = 'rsspal:account-oauth-flow'
export type AccountFlow = {
  popupChannel?: string
  owner: number | null
  binding: boolean
  returnTo: string
  createdAt: number
}
export function safeAccountReturn(v: unknown): string {
  if (
    typeof v !== 'string' ||
    !v.startsWith('/') ||
    v.startsWith('//') ||
    v.includes('\\')
  )
    return '/articles'
  try {
    const u = new URL(v, location.origin)
    return u.origin === location.origin &&
      ['/articles', '/feeds', '/capture', '/settings', '/explore'].includes(
        u.pathname,
      )
      ? u.pathname + u.search + u.hash
      : '/articles'
  } catch {
    return '/articles'
  }
}
export function readAccountFlow(): AccountFlow | null {
  try {
    const v = JSON.parse(sessionStorage.getItem(flowKey) || 'null')
    return v &&
      typeof v.binding === 'boolean' &&
      (v.owner === null || (Number.isSafeInteger(v.owner) && v.owner > 0)) &&
      Number.isFinite(v.createdAt) &&
      Date.now() - v.createdAt >= 0 &&
      Date.now() - v.createdAt < 600000
      ? { ...v, returnTo: safeAccountReturn(v.returnTo), ...(typeof v.popupChannel === 'string' && /^rsspal-qq-[a-f0-9-]{36}$/.test(v.popupChannel) ? {popupChannel: v.popupChannel} : {popupChannel: undefined}) }
      : null
  } catch {
    return null
  }
}
export function clearAccountFlow() {
  try {
    sessionStorage.removeItem(flowKey)
  } catch {}
}
export async function startAccountOAuth(
  provider: AccountProvider,
  returnTo: string,
  remember: boolean,
  binding = false,
): Promise<string> {
  const owner = currentAccountID()
  if (binding && owner === null) throw Error('请先登录后绑定')
  const flow: AccountFlow = {
    owner,
    binding,
    returnTo: safeAccountReturn(returnTo),
    createdAt: Date.now(),
  }
  const { data } = await (binding ? api : publicAuth).post(
    binding
      ? `/auth/identities/${provider}/start`
      : `/auth/oauth/${provider}/start`,
    { return_to: flow.returnTo, remember },
  )
  if (currentAccountID() !== owner) throw Error('账号已切换，请重新操作')
  const u = new URL(data.url)
  if (
    u.protocol !== 'https:' ||
    u.hostname !== {google: 'accounts.google.com', apple: 'appleid.apple.com', github: 'github.com', qq: 'graph.qq.com'}[provider]
  )
    throw Error('登录跳转无效，请重试')
  sessionStorage.setItem(flowKey, JSON.stringify(flow))
  return u.href
}
const completions = new Map<string, Promise<AccountAuthResult>>()
// StrictMode replay shares the one-use ticket request; credentials stay in memory.
export function exchangeAccountTicket(
  ticket: string,
  binding: boolean,
): Promise<AccountAuthResult> {
  const key = `${binding}:${currentAccountID()}:${ticket}`
  let result = completions.get(key)
  if (!result) {
    result = (binding ? api : publicAuth)
      .post<AccountAuthResult>('/auth/oauth/exchange', { ticket })
      .then((r) => r.data)
    completions.set(key, result)
    void result
      .finally(() => {
        setTimeout(() => completions.delete(key), 60000)
      })
      .catch(() => {})
  }
  return result
}
