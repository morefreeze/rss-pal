import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import { englifeBridge } from '../englife/bridge'

type State = 'setup_required' | 'disconnected' | 'ready' | 'expired' | 'quota_exhausted' | 'unavailable'
interface Status { state: State; configured: boolean; connection_id?: string; account?: string; checked_at?: string; remaining?: number; jobs: { video_id: string; status: string; updated_at: string }[] }
const base = '/admin/integrations/englife'
const jobLabels: Record<string, string> = { new: '等待提交', submitting: '正在提交', pending: '处理中', ready: '已完成', failed: '处理失败', needs_attention: '需要人工检查' }
const labels: Record<State, string> = { setup_required: '服务端尚未配置', disconnected: '未连接', ready: '已连接', expired: '登录已过期', quota_exhausted: '额度已用尽', unavailable: '暂时不可用' }
export default function EnglifeIntegrationPage({ user }: { user?: { is_admin: boolean } | null }) {
  const isAdmin = !!user?.is_admin
  const [status, setStatus] = useState<Status | null>(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [deadline, setDeadline] = useState<number | null>(null)
  const generation = useRef(0)
  const previousConnection = useRef('')
  useEffect(() => {
    const current = ++generation.current
    setBusy(false); setNotice('')
    if (!isAdmin) { setStatus(null); setDeadline(null); return }
    api.get<Status>(base).then(({ data }) => { if (generation.current === current) setStatus(data) }).catch(() => { if (generation.current === current) setNotice('读取连接状态失败，请重试。') })
    return () => { generation.current++ }
  }, [isAdmin])
  useEffect(() => {
    if (!isAdmin || !deadline) return
    let stopped = false
    let pending = false
    const current = generation.current
    const timer = window.setInterval(async () => {
      if (Date.now() >= deadline) { setDeadline(null); setNotice('授权请求已过期，请重新连接。'); return }
      if (pending) return
      pending = true
      try {
        const { data } = await api.get<Status>(base)
        if (stopped || current !== generation.current) return
        setStatus(data)
        if ((data.state === 'ready' || data.state === 'quota_exhausted') && !!data.connection_id && data.connection_id !== previousConnection.current) { setDeadline(null); setNotice('连接完成。') }
      } catch (_) { if (!stopped) setNotice('暂时无法读取状态，正在重试…') }
      finally { pending = false }
    }, 2000)
    return () => { stopped = true; window.clearInterval(timer) }
  }, [isAdmin, deadline])
  async function connect() {
    const current = ++generation.current
    setBusy(true); setNotice('')
    previousConnection.current = status?.connection_id ?? ''
    try {
      const ping = await englifeBridge('PING')
      if (current !== generation.current) return
      if (!ping.ok) throw new Error()
      const { data } = await api.post<{ token: string; expires_at: string }>(base + '/pair')
      if (current !== generation.current) return
      const authorization = await englifeBridge('AUTHORIZE', { token: data.token, serverOrigin: location.origin })
      if (current !== generation.current) return
      if (!authorization.ok) throw new Error()
      setDeadline(Date.parse(data.expires_at))
      setNotice('请在扩展授权页确认；此页将自动更新连接状态。')
    } catch (_) { if (current === generation.current) setNotice('无法发起授权。请安装或更新 RSS Pal 扩展并刷新此页后重试。') }
    finally { if (current === generation.current) setBusy(false) }
  }
  async function act(action: 'check' | 'disconnect') {
    const current = ++generation.current
    setBusy(true); setNotice(''); setDeadline(null)
    try {
      const { data } = action === 'check' ? await api.post<Status>(base + '/check', undefined, { timeout: 40000 }) : await api.delete<Status>(base)
      if (current !== generation.current) return
      setStatus(data)
      if (action === 'disconnect') setNotice('已断开连接并清除服务端保存的登录状态。')
    } catch (_) { if (current === generation.current) setNotice('操作失败，请稍后重试。') }
    finally { if (current === generation.current) setBusy(false) }
  }
  if (!isAdmin) return <div className="card">仅管理员可管理 englife 连接</div>
  return <div className="card" style={{ maxWidth: 820, margin: '0 auto' }}>
    <h1>englife 连接</h1>
    <p>连接站点共享的 englife 账号，供 RSS Pal 服务器处理视频。所有用户的视频任务共用该账号及额度。</p>
    <p>请先在此浏览器登录 englife，再授权扩展将 englife 登录 Cookie 直接交给本站服务器保存。不会读取 Google 登录凭据。</p>
    <p><a href="https://englife.space/en/" target="_blank" rel="noreferrer">打开 englife 登录</a> · <a href="/settings">在设置中安装/更新扩展</a></p>
    <p role="status">{status ? labels[status.state] : '正在读取状态…'}</p>
    {status?.account && <p>共享账号：{status.account}</p>}
    {status?.remaining !== undefined && <p>剩余额度：{status.remaining}</p>}
    {status?.checked_at && <p>最近检查：{new Date(status.checked_at).toLocaleString()}</p>}
    {status?.state === 'setup_required' && <p>请先按部署文档配置服务端加密密钥，再连接账号。</p>}
    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
      <button onClick={connect} disabled={busy || !!deadline || !status?.configured}>登录后授权连接</button>
      <button onClick={() => act('check')} disabled={busy || !!deadline || !status?.configured}>检查状态</button>
      <button onClick={() => act('disconnect')} disabled={busy || !status || status.state === 'setup_required'}>断开连接</button>
    </div>
    {notice && <p role="status">{notice}</p>}
    <h2>最近视频任务</h2>
    {status?.jobs?.some(job => job.status === 'needs_attention') && <p>需要人工检查的任务不会自动重试，以避免重复扣费。请打开 englife 查看对应视频的处理结果，再决定后续操作。</p>}
    {status?.jobs?.length ? <ul>{status.jobs.map(job => <li key={job.video_id}>{job.video_id}：{jobLabels[job.status] ?? '未知状态'} · {new Date(job.updated_at).toLocaleString()}</li>)}</ul> : <p>暂无任务</p>}
  </div>
}
