import { useEffect, useState } from 'react'
import { getShortcutConnections, pairShortcut, revokeShortcutConnections } from '../api/client'

type Pairing = { code: string; expires_at: string }

export default function ShortcutConnection() {
  const [count, setCount] = useState<number | null>(null)
  const [pair, setPair] = useState<Pairing | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [expired, setExpired] = useState(false)
  const refresh = () => getShortcutConnections().then(setCount).catch(() => setCount(null))

  useEffect(() => { void refresh() }, [])
  useEffect(() => {
    const onFocus = () => { void refresh() }
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [])
  useEffect(() => {
    if (!pair) return
    const timer = window.setTimeout(() => {
      setPair(null)
      setExpired(true)
    }, Math.max(0, Date.parse(pair.expires_at) - Date.now()))
    return () => window.clearTimeout(timer)
  }, [pair])

  const connect = async () => {
    setBusy(true)
    setPair(null)
    setError('')
    setMessage('')
    setExpired(false)
    try {
      const result = await pairShortcut()
      if (!/^[a-f0-9]{64}$/.test(result.code) || !Number.isFinite(Date.parse(result.expires_at)) || Date.parse(result.expires_at) <= Date.now()) {
        throw new Error('invalid connection')
      }
      setPair(result)
    } catch {
      setError('创建连接失败，请重试。')
    } finally {
      setBusy(false)
    }
  }
  const revoke = async () => {
    setBusy(true)
    setError('')
    try {
      await revokeShortcutConnections()
      setCount(0)
      setPair(null)
      setMessage('已断开连接，旧快捷指令将无法提交抓取。')
    } catch {
      setError('断开连接失败，请重试。')
    } finally {
      setBusy(false)
    }
  }
  // Custom URL schemes use percent-encoded spaces, matching Apple's run URL.
  const nativeURL = pair
    ? `shortcuts://run-shortcut?name=${encodeURIComponent('加入 RSS Pal')}&input=text&text=${encodeURIComponent(`rsspal-pair:${pair.code}`)}`
    : ''

  return <div style={{ marginTop: 12 }}>
    <p className="text-sm mb-2">安装后连接一次当前账号。以后“共享 → 加入 RSS Pal”即可直接保存到网摘，不需要在 Chrome 里另外登录。</p>
    {count !== null && count > 0 && <p role="status" className="text-sm mb-2">已连接 {count} 个快捷指令</p>}
    <button type="button" disabled={busy} onClick={() => void connect()} style={{ minHeight: 44 }}>连接当前账号</button>
    {(count === null || count > 0 || pair) && <button type="button" disabled={busy} onClick={() => void revoke()} style={{ minHeight: 44, marginLeft: 8 }}>断开所有快捷指令</button>}
    {pair && <p style={{ marginTop: 8 }}>
      <a href={nativeURL}>打开快捷指令完成连接</a>
      <span className="text-muted text-sm">（五分钟内有效，仅可使用一次）</span>
    </p>}
    {expired && <p role="status">连接已过期，请重新点“连接当前账号”。</p>}
    {error && <p role="alert">{error}</p>}
    {message && <p role="status">{message}</p>}
  </div>
}
