import { useState, useEffect } from 'react'
import { useLocation, useNavigate, Link } from 'react-router-dom'
import { login, api } from '../api/client'
import { authSearch, parseAuthIntent, postAuthURL } from '../utils/authIntent'

import VerifiedAccountAccess from '../components/VerifiedAccountAccess'
import { getAuthMethods, type AuthMethods } from '../api/accountAuth'
import { authErrorMessage } from '../utils/authError'

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
  const verified = Boolean(methods && (methods.email || methods.google || methods.apple || methods.github))

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

  return (
    <div className="card" style={{ maxWidth: 400, margin: '100px auto', padding: '0 16px', width: '100%' }}>
      <h2 style={{ marginBottom: 16 }}>RSS Pal</h2>

      {verified && methods && <VerifiedAccountAccess
        methods={methods} mode="login" returnTo={postAuthURL(intent)}
        onSuccess={user => { onLogin(user); navigate(postAuthURL(intent), { replace: true }) }}
      />}
      {verified && <button type="button" className="secondary" style={{ width: '100%', margin: '16px 0' }} onClick={() => setShowPassword(value => !value)}>
        {showPassword ? '收起密码登录' : '使用账号密码登录'}
      </button>}
      {(!verified || showPassword) && <form onSubmit={handleSubmit}>
        <div className="mb-2">
          <input
            type="text"
            placeholder="用户名"
            value={username}
            onChange={e => setUsername(e.target.value)}
            autoComplete="username"
            disabled={submitting}
          />
        </div>
        <div className="mb-2">
          <input
            type="password"
            placeholder="密码"
            value={password}
            onChange={e => setPassword(e.target.value)}
            autoComplete="current-password"
            disabled={submitting}
          />
        </div>
        <label className="text-sm mb-2" style={{ display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer' }}>
          <input
            type="checkbox"
            checked={remember}
            onChange={e => setRemember(e.target.checked)}
            disabled={submitting}
            style={{ width: 'auto' }}
          />
          <span>记住此设备 90 天</span>
        </label>
        {firstRun && (
          <div className="text-sm mb-2" style={{ color: '#2e7d32', background: '#f1f8e9', padding: '8px', borderRadius: 4 }}>
            首次使用，已自动创建管理员账号。用户名：admin，请使用管理员配置的密码登录
          </div>
        )}
        {error && <div className="text-sm mb-2" style={{ color: 'red' }}>{error}</div>}
        <button type="submit" style={{ width: '100%' }} disabled={submitting}>
          {submitting ? '登录中...' : '登录'}
        </button>
      </form>}
        <div style={{ textAlign: 'center', marginTop: 12 }}>
          <Link to={`/register${authSearch(intent)}`}>
            <button type="button" className="secondary">{methods?.public_registration ? '新用户注册' : intent.shareRef ? '通过分享邀请注册' : '使用邀请码注册'}</button>
          </Link>
        </div>

      <p className="text-sm" style={{ textAlign: 'center', marginTop: 16 }}><a href="/privacy.html" target="_blank" rel="noopener noreferrer">隐私政策</a></p>
    </div>
  )
}
