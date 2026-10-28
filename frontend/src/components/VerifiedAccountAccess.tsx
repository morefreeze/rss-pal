import { useEffect, useRef, useState, type ReactNode } from 'react'
import { getRegistrationConfig } from '../api/client'
import {
  currentAccountID,
  persistAccountSession,
  requestEmailCode,
  verifyEmailCode,
  startAccountOAuth,
  type AuthMethods,
  type AccountProvider,
} from '../api/accountAuth'
import { verifyTencentCaptcha } from '../utils/tencentCaptcha'
import RegistrationChallenge from './RegistrationChallenge'
import { authErrorMessage } from '../utils/authError'
import AccountProviderIcon from './AccountProviderIcon'
type Props = {
  methods: AuthMethods
  mode: 'login' | 'register' | 'bind'
  returnTo: string
  children?: ReactNode
  onSuccess?: (user: {
    id: number
    username: string
    is_admin: boolean
  }) => void
  onBound?: () => void
}
export default function VerifiedAccountAccess({
  methods,
  mode,
  returnTo,
  onSuccess,
  onBound,
  children,
}: Props) {
  const binding = mode === 'bind'
  const [email, setEmail] = useState(''),
    [code, setCode] = useState(''),
    [challenge, setChallenge] = useState(''),
    [cooldown, setCooldown] = useState(0),
    [remember, setRemember] = useState(true),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [sent, setSent] = useState(false)
  const [captcha, setCaptcha] = useState<{
      available: boolean
      site_key: string
      provider: string
    } | null>(null),
    [proof, setProof] = useState(''),
    [attempt, setAttempt] = useState(0)
  const mountedOwner = useRef(currentAccountID())
  const generation = useRef(0),
    inflight = useRef(false),
    controller = useRef<AbortController | null>(null)
  useEffect(() => {
    let active = true
    if (methods.email && !binding)
      getRegistrationConfig()
        .then((v) => {
          if (active) setCaptcha(v)
        })
        .catch(() => {
          if (active) setCaptcha(null)
        })
    return () => {
      active = false
      generation.current++
      controller.current?.abort()
    }
  }, [methods.email, binding])
  useEffect(() => {
    if (cooldown <= 0) return
    const timer = setTimeout(() => setCooldown((v) => Math.max(0, v - 1)), 1000)
    return () => clearTimeout(timer)
  }, [cooldown])
  const send = async () => {
    if (inflight.current) return
    if (binding && currentAccountID() !== mountedOwner.current) {
      setError('账号已切换，请刷新页面后重试')
      return
    }
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) {
      setError('请填写有效邮箱')
      return
    }
    const owner = currentAccountID(),
      run = ++generation.current
    inflight.current = true
    setBusy(true)
    setError('')
    const active = () =>
      generation.current === run && currentAccountID() === owner
    try {
      controller.current = new AbortController()
      const answer = binding
        ? ''
        : captcha?.provider === 'tencent'
          ? await verifyTencentCaptcha(
              captcha.site_key,
              controller.current.signal,
            )
          : proof
      if (!active()) return
      const result = await requestEmailCode(email.trim(), answer, binding)
      if (active()) {
        setChallenge(result.challenge_id)
        setSent(true)
        setCode('')
        setCooldown(Math.max(1, Math.min(300, result.retry_after || 60)))
      }
    } catch (e) {
      if (active()) setError(authErrorMessage(e, '验证码发送失败，请稍后重试'))
    } finally {
      if (generation.current === run) {
        inflight.current = false
        setBusy(false)
        setProof('')
        setAttempt((v) => v + 1)
      }
    }
  }
  const verify = async (e: React.FormEvent) => {
    e.preventDefault()
    if (inflight.current || !challenge || !/^\d{6}$/.test(code)) return
    if (binding && currentAccountID() !== mountedOwner.current) {
      setError('账号已切换，请刷新页面后重试')
      return
    }
    const owner = currentAccountID(),
      run = ++generation.current
    inflight.current = true
    setBusy(true)
    setError('')
    try {
      const result = await verifyEmailCode(challenge, code, remember, binding)
      if (generation.current !== run || currentAccountID() !== owner) return
      if (binding) {
        if (!('bound' in result) || !result.bound)
          throw Error('绑定结果无效，请重试')
        onBound?.()
      } else {
        if (!('token' in result)) throw Error('登录结果无效，请重试')
        persistAccountSession(result)
        onSuccess?.(result.user)
      }
    } catch (e) {
      if (generation.current === run && currentAccountID() === owner)
        setError(authErrorMessage(e, '验证码无效或已过期，请重试'))
    } finally {
      if (generation.current === run) {
        inflight.current = false
        setBusy(false)
      }
    }
  }
  const oauth = async (provider: AccountProvider) => {
    if (inflight.current) return
    if (binding && currentAccountID() !== mountedOwner.current) {
      setError('账号已切换，请刷新页面后重试')
      return
    }
    const run = ++generation.current
    inflight.current = true
    setBusy(true)
    setError('')
    try {
      const url = await startAccountOAuth(provider, returnTo, remember, binding)
      if (generation.current === run) window.location.assign(url)
    } catch (e) {
      if (generation.current === run)
        setError(authErrorMessage(e, '第三方登录暂时不可用，请重试'))
    } finally {
      if (generation.current === run) {
        inflight.current = false
        setBusy(false)
      }
    }
  }
  const emailReady =
    binding ||
    Boolean(
      captcha?.available &&
      captcha.site_key &&
      (captcha.provider === 'tencent' || proof),
    )
  return (
    <section
      className="verified-account-access"
      aria-label={binding ? '绑定登录方式' : '验证登录'}
    >
      {!binding && mode !== 'login' && (
        <label
          style={{
            display: 'flex',
            gap: 6,
            alignItems: 'center',
            margin: '12px 0',
          }}
        >
          <input
            type="checkbox"
            checked={remember}
            onChange={(e) => setRemember(e.target.checked)}
            style={{ width: 'auto' }}
          />
          记住此设备 90 天
        </label>
      )}
      {methods.email && (
        <form onSubmit={verify}>
          <label htmlFor="verified-email">邮箱</label>
          <input
            id="verified-email"
            type="email"
            autoComplete="email"
            inputMode="email"
            value={email}
            disabled={busy}
            onChange={(e) => {
              generation.current++
              setEmail(e.target.value)
              setChallenge('')
              setCode('')
              setSent(false)
              setCooldown(0)
              setError('')
            }}
          />
          <div
            style={{ display: 'flex', gap: 8, alignItems: 'end', marginTop: mode === 'login' ? 16 : 8 }}
          >
            <div style={{ flex: 1, minWidth: 0 }}>
              <label htmlFor="verified-code">邮箱验证码</label>
              <input
                id="verified-code"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                value={code}
                disabled={busy}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
              />
            </div>
            <button
              type="button"
              className="secondary"
              disabled={busy || cooldown > 0 || !emailReady}
              onClick={() => void send()}
            >
              {cooldown > 0 ? `${cooldown} 秒后重发` : '发送验证码'}
            </button>
          </div>
          {captcha?.provider === 'turnstile' && !binding && (
            <RegistrationChallenge
              key={attempt}
              siteKey={captcha.site_key}
              onVerify={setProof}
            />
          )}
          {sent && (
            <p role="status" className="text-sm">
              验证码已发送，请检查收件箱和垃圾邮件。
            </p>
          )}
          {mode === 'login' && (
            <label className="account-remember">
              <input type="checkbox" checked={remember} disabled={busy} onChange={e => setRemember(e.target.checked)} />
              记住此设备 90 天
            </label>
          )}
          <button
            type="submit"
            style={{ width: '100%' }}
            disabled={busy || !challenge || !/^\d{6}$/.test(code)}
          >
            {busy
              ? '处理中…'
              : binding
                ? '验证并绑定'
                : mode === 'register'
                  ? '验证并注册'
                  : '验证并登录'}
          </button>
        </form>
      )}
      {mode === 'login' && !methods.email && (
        <label className="account-remember">
          <input type="checkbox" checked={remember} disabled={busy} onChange={e => setRemember(e.target.checked)} />
          记住此设备 90 天
        </label>
      )}
      {children}
      {(['google', 'github', 'qq', 'apple'] as const).some(provider => methods[provider]) && (
        <div className={binding ? 'account-provider-bindings' : 'account-provider-logins'}>
          {!binding && <div className="account-provider-divider">其他登录方式</div>}
          <div className="account-provider-buttons" role="group" aria-label={binding ? '第三方账号绑定' : '第三方登录'}>
            {(['google', 'github', 'qq', 'apple'] as const).filter(provider => methods[provider]).map(provider => {
              const name = { google: 'Google', github: 'GitHub', qq: 'QQ', apple: 'Apple' }[provider]
              const label = binding ? `绑定 ${name}` : `使用 ${name} 继续`
              return (
                <button
                  key={provider}
                  type="button"
                  className={`secondary${binding ? '' : ' account-provider-icon'}`}
                  aria-label={label}
                  title={label}
                  disabled={busy}
                  onClick={() => void oauth(provider)}
                >
                  <AccountProviderIcon provider={provider} />
                  {binding && <span>{label}</span>}
                </button>
              )
            })}
          </div>
        </div>
      )}
      {!methods.email && !methods.google && !methods.github && !methods.qq && !methods.apple && (
        <p className="text-muted">验证登录暂时不可用，请稍后再试。</p>
      )}
      {error && (
        <p
          role="alert"
          className="text-sm"
          style={{ color: 'var(--danger,#b91c1c)' }}
        >
          {error}
        </p>
      )}
    </section>
  )
}
