import { useEffect, useState } from 'react'
import { useLocation, useNavigate, Link } from 'react-router-dom'
import { getRegistrationConfig, register } from '../api/client'
import { authSearch, parseAuthIntent, postAuthURL } from '../utils/authIntent'

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
  const [proof, setProof] = useState('')
  const [challengeAttempt, setChallengeAttempt] = useState(0)
  useEffect(() => {
    let disposed = false
    getRegistrationConfig().then(config => {
      if (disposed) return
      if (config.available && config.site_key) setSiteKey(config.site_key)
      else setError('注册验证暂时不可用，请稍后重试')
    }).catch(() => { if (!disposed) setError('注册验证暂时不可用，请稍后重试') })
    return () => { disposed = true }
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (password.length < 6) {
      setError('密码至少 6 位')
      return
    }

    if (!siteKey || !proof || submitting) return
    setSubmitting(true)

    try {
      const data = await register(username, password, code, proof)
      onLogin(data.user)
      navigate(postAuthURL(intent), { replace: true })
    } catch (err: any) {
      setError(authErrorMessage(err, '注册失败'))
    } finally {
      setProof('')
      setChallengeAttempt(value => value + 1)
      setSubmitting(false)
    }
  }

  return (
    <div className="card" style={{ maxWidth: 400, margin: '100px auto', padding: '0 16px', width: '100%' }}>
      <h2 style={{ marginBottom: 16 }}>RSS Pal - 注册</h2>

      <form onSubmit={handleSubmit}>
        <div className="mb-2">
          <input
            type="text"
            placeholder="邀请码"
            value={code}
            onChange={e => setCode(e.target.value)}
            disabled={submitting}
          />
        </div>
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
        {siteKey && <RegistrationChallenge key={challengeAttempt} siteKey={siteKey} onVerify={setProof} />}
        {error && <div className="text-sm mb-2" style={{ color: 'red' }}>{error}</div>}
        <button type="submit" style={{ width: '100%' }} disabled={submitting || !siteKey || !proof}>
          {submitting ? '注册中...' : '注册'}
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
