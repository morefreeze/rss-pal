import { useEffect, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import {
  clearAccountFlow,
  currentAccountID,
  exchangeAccountTicket,
  persistAccountSession,
  readAccountFlow,
  safeAccountReturn,
} from '../api/accountAuth'
import { authErrorMessage } from '../utils/authError'
export default function AccountAuthComplete({
  onLogin,
}: {
  onLogin: (user: { id: number; username: string; is_admin: boolean }) => void
}) {
  const location = useLocation(),
    navigate = useNavigate()
  const [initial] = useState(() => ({
    ticket: new URLSearchParams(location.search).get('ticket'),
    flow: readAccountFlow(),
  }))
  const [error, setError] = useState('')
  useEffect(() => {
    let disposed = false
    // Strip the short-lived credential from history before subsequent navigation.
    if (window.location.pathname === '/auth/complete')
      window.history.replaceState(window.history.state, '', '/auth/complete')
    const { ticket, flow } = initial
    if (
      !ticket ||
      ticket.length > 256 ||
      !flow ||
      flow.owner !== currentAccountID()
    ) {
      setError('登录未完成或账号已切换，请重新尝试。')
      return
    }
    exchangeAccountTicket(ticket, flow.binding)
      .then((result) => {
        if (disposed) return
        if (currentAccountID() !== flow.owner)
          throw Error('账号已切换，请重新尝试。')
        if (flow.binding) {
          if (!('bound' in result) || !result.bound)
            throw Error('绑定未完成，请重试。')
        } else {
          if (!('token' in result)) throw Error('登录未完成，请重试。')
          persistAccountSession(result)
          onLogin(result.user)
        }
        clearAccountFlow()
        navigate(safeAccountReturn(result.return_to || flow.returnTo), {
          replace: true,
        })
      })
      .catch((e) => {
        if (!disposed) setError(authErrorMessage(e, '登录未完成，请重新尝试。'))
      })
    return () => {
      disposed = true
    }
  }, [initial, navigate, onLogin])
  return (
    <div
      className="card account-access-card"
      style={{ maxWidth: 400, margin: '60px auto', padding: 20 }}
    >
      <h2>{initial.flow?.binding ? '绑定登录方式' : '完成登录'}</h2>
      {error ? (
        <>
          <p role="alert">{error}</p>
          <Link to={initial.flow?.binding ? '/settings#account' : '/login'}>
            返回并重试
          </Link>
        </>
      ) : (
        <p role="status">正在安全完成验证…</p>
      )}
    </div>
  )
}
