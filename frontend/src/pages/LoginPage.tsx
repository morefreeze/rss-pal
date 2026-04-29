import { useState, useEffect } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { login, api } from '../api/client'

interface LoginPageProps {
  onLogin: (user: any) => void
}

export default function LoginPage({ onLogin }: LoginPageProps) {
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [firstRun, setFirstRun] = useState(false)

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
      const data = await login(username, password)
      onLogin(data.user)
      navigate('/articles', { replace: true })
    } catch {
      setError('用户名或密码错误')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="card" style={{ maxWidth: 400, margin: '100px auto' }}>
      <h2 style={{ marginBottom: 16 }}>RSS Pal</h2>

      <form onSubmit={handleSubmit}>
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
        {firstRun && (
          <div className="text-sm mb-2" style={{ color: '#2e7d32', background: '#f1f8e9', padding: '8px', borderRadius: 4 }}>
            首次使用，已自动创建管理员账号。用户名：admin，密码为 AUTH_PASSWORD（默认 admin）
          </div>
        )}
        {error && <div className="text-sm mb-2" style={{ color: 'red' }}>{error}</div>}
        <button type="submit" style={{ width: '100%' }} disabled={submitting}>
          {submitting ? '登录中...' : '登录'}
        </button>
        <div style={{ textAlign: 'center', marginTop: 12 }}>
          <Link to="/register">
            <button type="button" className="secondary">使用邀请码注册</button>
          </Link>
        </div>
      </form>
    </div>
  )
}
