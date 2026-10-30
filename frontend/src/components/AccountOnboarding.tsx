import type { User } from '../App'
import { useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { AccountSetupModal, NicknameOnboarding } from './NicknameSetup'

export default function AccountOnboarding({ user, onSaved }: { user: User; onSaved: (user: User) => void }) {
  const location = useLocation()
  const navigate = useNavigate()
  const key = `rsspal:password-skipped:${user.id}`
  const [dismissed, setDismissed] = useState(false)
  useEffect(() => {
    if (user.needs_password && location.pathname === '/settings') {
      sessionStorage.setItem(key, '1')
      setDismissed(true)
    }
  }, [key, user.needs_password, location.pathname])
  // A password prompt owns this login's onboarding, even after the user sets it.
  if (dismissed || sessionStorage.getItem(key) || location.pathname === '/settings') return null
  if (user.needs_password) {
    const dismiss = () => { sessionStorage.setItem(key, '1'); setDismissed(true) }
    return <AccountSetupModal title="设置登录密码" onClose={dismiss}>
      <p className="mb-2">你还没有设置登录密码。设置后，可使用用户名和密码登录。</p>
      <div className="flex gap-2" style={{ flexWrap: 'wrap' }}>
        <button type="button" onClick={() => { dismiss(); navigate('/settings#account') }}>去设置密码</button>
        <button type="button" className="secondary" onClick={dismiss}>稍后设置</button>
      </div>
    </AccountSetupModal>
  }
  return <NicknameOnboarding user={user} onSaved={onSaved} />
}
