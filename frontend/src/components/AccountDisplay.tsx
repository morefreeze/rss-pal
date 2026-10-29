import { useEffect, useState } from 'react'
export type DisplayAccount = {
  username: string
  nickname?: string
  qq_nickname?: string
  qq_avatar_url?: string
}
function safeAvatar(raw?: string): string | null {
  if (!raw) return null
  try {
    const u = new URL(raw)
    if (u.protocol !== 'https:' || u.username || u.password || u.port ||
      (u.hostname !== 'qlogo.cn' && !u.hostname.endsWith('.qlogo.cn'))) return null
    return u.href
  } catch { return null }
}
export default function AccountDisplay({user}: {user: DisplayAccount}) {
  const avatar = safeAvatar(user.qq_avatar_url)
  const [failed, setFailed] = useState(false)
  useEffect(() => setFailed(false), [avatar])
  const name = user.nickname || user.qq_nickname || user.username
  return <span className="account-display">
    {avatar && !failed ? <img src={avatar} width={24} height={24} alt="QQ 头像" referrerPolicy="no-referrer" loading="lazy" decoding="async" onError={() => setFailed(true)} /> : <span aria-hidden="true">👤</span>}
    <span className="account-display-name">{name}{user.nickname && user.qq_nickname && user.nickname !== user.qq_nickname && <span className="text-muted">（QQ：{user.qq_nickname}）</span>}</span>
  </span>
}
