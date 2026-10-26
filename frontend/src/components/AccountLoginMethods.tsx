import { useEffect, useRef, useState } from 'react'
import {
  getAccountIdentities,
  getAuthMethods,
  currentAccountID,
  type AccountIdentity,
  type AuthMethods,
} from '../api/accountAuth'
import VerifiedAccountAccess from './VerifiedAccountAccess'
export default function AccountLoginMethods() {
  const [methods, setMethods] = useState<AuthMethods | null>(null),
    [identities, setIdentities] = useState<AccountIdentity[]>([]),
    [error, setError] = useState(''),
    [loaded, setLoaded] = useState(false)
  const owner = useRef(currentAccountID()),
    generation = useRef(0)
  const load = async () => {
    const run = ++generation.current
    try {
      const [m, i] = await Promise.all([
        getAuthMethods(),
        getAccountIdentities(),
      ])
      if (generation.current !== run || currentAccountID() !== owner.current)
        return
      setMethods(m)
      setIdentities(i.identities || [])
      setError('')
      setLoaded(true)
    } catch {
      if (generation.current === run) setError('登录方式加载失败，请重试')
    }
  }
  useEffect(() => {
    void load()
    return () => {
      generation.current++
    }
  }, [])
  if (error)
    return (
      <div className="card mb-2">
        <p role="alert">{error}</p>
        <button className="secondary" onClick={() => void load()}>
          重试
        </button>
      </div>
    )
  if (!loaded || !methods) return null
  const available = {
    ...methods,
    email: methods.email && !identities.some((i) => i.provider === 'email'),
    github: methods.github && !identities.some((i) => i.provider === 'github'),
    google: methods.google && !identities.some((i) => i.provider === 'google'),
    apple: methods.apple && !identities.some((i) => i.provider === 'apple'),
  }
  return (
    <div className="card mb-2">
      <h3 className="mb-2">登录方式</h3>
      <p className="text-sm">
        绑定后可使用这些方式登录当前账号，订阅和阅读记录保持不变。
      </p>
      {identities.length > 0 && (
        <ul>
          {identities.map((i) => (
            <li key={i.provider}>
              {i.provider === 'email'
                ? '邮箱'
                : i.provider === 'github'
                  ? 'GitHub'
                  : i.provider === 'google'
                    ? 'Google'
                    : 'Apple'}
              ：已绑定{i.email ? `（${i.email}）` : ''}
            </li>
          ))}
        </ul>
      )}
      {(available.email || available.google || available.apple || available.github) && (
        <VerifiedAccountAccess
          methods={available}
          mode="bind"
          returnTo="/settings#account"
          onBound={() => void load()}
        />
      )}
    </div>
  )
}
