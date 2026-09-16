import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, Link } from 'react-router-dom'
import { getRegistrationConfig, register } from '../api/client'
import { authSearch, parseAuthIntent, postAuthURL } from '../utils/authIntent'

import { verifyTencentCaptcha } from '../utils/tencentCaptcha'
import RegistrationChallenge from '../components/RegistrationChallenge'
import { authErrorMessage } from '../utils/authError'

interface RegisterPageProps {
  onLogin: (user: any) => void
}

export default function RegisterPage({ onLogin }: RegisterPageProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const intent = parseAuthIntent(location.search)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [siteKey, setSiteKey] = useState('')
  const [provider, setProvider] = useState('turnstile')
  const challengeController = useRef<AbortController | null>(null)
  const inFlight = useRef(false)
  useEffect(() => () => { challengeController.current?.abort() }, [])
  const [proof, setProof] = useState('')
  const [challengeAttempt, setChallengeAttempt] = useState(0)
  useEffect(() => {
    let disposed = false
    getRegistrationConfig().then(config => {
      if (disposed) return
      if (config.available && config.site_key && (!config.provider || ['tencent','turnstile'].includes(config.provider))) { setSiteKey(config.site_key); setProvider(config.provider || 'turnstile') }
      else setError('注册验证暂时不可用，请稍后重试')
    }).catch(() => { if (!disposed) setError('注册验证暂时不可用，请稍后重试') })
    return () => { disposed = true }
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (!username.trim() || [...username].length > 64) {
      setError('请填写用户名，最多 64 个字符')
      return
    }
    if (!intent.shareRef && (!code.trim() || [...code].length > 32)) {
      setError('请填写有效邀请码，最多 32 个字符')
      return
    }
    if (new TextEncoder().encode(password).length > 72) {
      setError('密码不能超过 72 字节')
      return
    }
    if ([...password].length < 6) {
      setError('密码至少 6 位')
      return
    }

    if (!siteKey || (provider !== 'tencent' && !proof) || inFlight.current) return
    inFlight.current = true
    setSubmitting(true)

    try {
      challengeController.current = new AbortController()
      const response = provider === 'tencent' ? await verifyTencentCaptcha(siteKey, challengeController.current.signal) : proof
      if (challengeController.current.signal.aborted) return
      const data = intent.shareRef
        ? await register(username, password, '', response, intent.shareRef)
        : await register(username, password, code, response)
      onLogin(data.user)
      navigate(postAuthURL(intent), { replace: true })
    } catch (err: any) {
      setError(authErrorMessage(err, err instanceof Error ? err.message : '注册失败'))
    } finally {
      inFlight.current = false
      setProof('')
      setChallengeAttempt(value => value + 1)
      setSubmitting(false)
    }
  }

  return (
    <div className="card" style={{ maxWidth: 400, margin: '100px auto', padding: '0 16px', width: '100%' }}>
      <h2 style={{ marginBottom: 16 }}>RSS Pal - 注册</h2>

      <form onSubmit={handleSubmit}>
        {intent.shareRef ? <div className="mb-2 text-sm">
          <p>通过分享邀请注册，无需另填邀请码</p>
          <Link to={`/register${authSearch({...intent, shareRef: undefined})}`}>改用邀请码</Link>
        </div> : <div className="mb-2">
          <input
            type="text"
            placeholder="邀请码"
            value={code}
            onChange={e => setCode(e.target.value)}
            disabled={submitting}
          />
        </div>}
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
            placeholder="密码（至少 6 位）"
            value={password}
            onChange={e => setPassword(e.target.value)}
            autoComplete="new-password"
            disabled={submitting}
          />
        </div>
        {siteKey && provider === 'turnstile' && <RegistrationChallenge key={challengeAttempt} siteKey={siteKey} onVerify={setProof} />}
        {error && <div className="text-sm mb-2" style={{ color: 'red' }}>{error}</div>}
        <button type="submit" style={{ width: '100%' }} disabled={submitting || !siteKey || (provider !== 'tencent' && !proof)}>
          {submitting ? '验证并注册中...' : '注册'}
        </button>
        <div style={{ textAlign: 'center', marginTop: 12 }}>
          <Link to={`/login${authSearch(intent)}`}>
            <button type="button" className="secondary">已有账号？登录</button>
          </Link>
        </div>
      </form>
    </div>
  )
}
