import { useState, useEffect } from 'react'
import { useLocation, useNavigate, Link } from 'react-router-dom'
import { login, api } from '../api/client'
import { authSearch, parseAuthIntent, postAuthURL } from '../utils/authIntent'

import VerifiedAccountAccess from '../components/VerifiedAccountAccess'
import { getAuthMethods, type AuthMethods } from '../api/accountAuth'
import { authErrorMessage } from '../utils/authError'
import './LoginPage.css'

interface LoginPageProps {
  onLogin: (user: any) => void
}

export default function LoginPage({ onLogin }: LoginPageProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const intent = parseAuthIntent(location.search)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  // Default ON — most users will want this; the "log in once per device for
  // 90 days" tradeoff matches the workflow rss-pal is built for.
  const [remember, setRemember] = useState(true)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [firstRun, setFirstRun] = useState(false)
  const [methods,setMethods]=useState<AuthMethods|null>(null)
  const [showPassword,setShowPassword]=useState(false)
  useEffect(()=>{let active=true;getAuthMethods().then(m=>{if(active)setMethods(m)}).catch(()=>{});return()=>{active=false}},[])
  const verified = Boolean(methods && (methods.email || methods.google || methods.apple || methods.github || methods.qq))

  useEffect(() => {
    api.post('/auth/init').then(() => {
      setFirstRun(true)
      setUsername('admin')
    }).catch(() => {
      // admin already exists or error, ignore
    })
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setSubmitting(true)

    try {
      const data = await login(username, password, remember)
      onLogin(data.user)
      navigate(postAuthURL(intent), { replace: true })
    } catch (err) {
      setError(authErrorMessage(err, '用户名或密码错误'))
    } finally {
      setSubmitting(false)
    }
  }

  const passwordAccess = (
    <>
      {verified && <button type="button" className="login-text-action" aria-expanded={showPassword} aria-controls="password-login" onClick={() => setShowPassword(value => !value)}>
        {showPassword ? '收起密码登录' : '使用账号密码登录'}
      </button>}
      {(!verified || showPassword) && <form id="password-login" className="login-password-form" onSubmit={handleSubmit}>
        <label htmlFor="login-username">用户名</label>
        <input id="login-username" placeholder="用户名" type="text" value={username} onChange={e => setUsername(e.target.value)} autoComplete="username" disabled={submitting} />
        <label htmlFor="login-password">密码</label>
        <input id="login-password" placeholder="密码" type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="current-password" disabled={submitting} />
        <label className="account-remember">
          <input type="checkbox" checked={remember} onChange={e => setRemember(e.target.checked)} disabled={submitting} />
          <span>记住此设备 90 天</span>
        </label>
        {firstRun && <p className="login-notice">首次使用，已自动创建管理员账号。用户名：admin，请使用管理员配置的密码登录</p>}
        {error && <p role="alert" className="login-error">{error}</p>}
        <button type="submit" disabled={submitting}>{submitting ? '登录中...' : '登录'}</button>
      </form>}
    </>
  )

  return (
    <main className="login-page">
      <div className="login-shell">
        <header className="login-heading">
          <div className="login-brand"><img src="/favicon.svg" width={32} height={32} alt="" /><span>RSS Pal</span></div>
          <h1>登录 RSS Pal</h1>
          <p>继续阅读你关心的内容。</p>
        </header>
        {verified && methods ? <VerifiedAccountAccess
          methods={methods} mode="login" returnTo={postAuthURL(intent)}
          onSuccess={user => { onLogin(user); navigate(postAuthURL(intent), { replace: true }) }}
        >{passwordAccess}</VerifiedAccountAccess> : passwordAccess}
        <footer className="login-footer">
          <p>还没有账号？ <Link to={`/register${authSearch(intent)}`}>{methods?.public_registration ? '注册账号' : intent.shareRef ? '通过分享邀请注册' : '使用邀请码注册'}</Link></p>
          <a className="login-privacy" href="/privacy.html" target="_blank" rel="noopener noreferrer">隐私政策</a>
        </footer>
      </div>
    </main>
  )
}
