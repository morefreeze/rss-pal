import { useEffect, useRef, useState, type ReactNode } from 'react'
import { getAdminMonitoring, type MonitoringResponse } from '../api/monitoring'
import './AdminMonitoringPage.css'

const labels: Record<string, string> = {
  register: '注册', registration: '注册', login: '登录', refresh: '登录续期', captcha: '验证码',
  success: '成功', rejected: '未通过', unavailable: '服务不可用', invalid: '无效请求',
  limit: '限流', failed: '失败', network: '来源网络频率限制', account: '账号频率限制', global: '全局频率限制',
  user_concurrency: '用户并发限制', global_concurrency: '全局并发限制',
  explore_fetch_queue: '探索抓取', explore_related_tasks: '探索关联任务',
  rate_limit: '认证限流', auth_limit: '认证限流', task_limit: '任务限流', task_denied: '任务限流',
  user_daily: '用户每日额度', global_daily: '全局每日额度', user_concurrent: '用户并发限制',
  global_concurrent: '全局并发限制', concurrency: '并发限制', ai: 'AI 调用',
  fetch: '抓取', interactive: '交互任务', capture: '网摘', pdf: 'PDF', subscribe: '订阅',
  background_fetch: '后台抓取', background_ocr: '后台 OCR', summary: '摘要',
  explore: '探索', explore_fetch: '探索抓取', explore_validation: '探索验证',
}
const label = (key: string) => labels[key] || key || '—'
const when = (value?: string | null) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '尚无数据'
const number = (value?: number | null) => value == null ? '不可用' : value.toLocaleString('zh-CN')
const percent = (value: number) => `${(value * 100).toFixed(1)}%`
const amount = (value: number | null) => value == null ? '未配置单价' : `¥${value.toFixed(2)}`
const account = (id: number) => id > 0 ? `用户 #${id}` : '系统／未登录'
function alertValue(code: string, value: number): string {
  if (code === 'captcha_unavailable' || code.startsWith('quota_')) return percent(value)
  if (code.startsWith('queue_')) return `${Math.ceil(value / 60)} 分钟`
  if (code.includes('cost')) return amount(value)
  return number(value)
}
function Table({ headers, children }: { headers: string[]; children: ReactNode }) {
  return <div className="monitor-table-wrap"><table><thead><tr>{headers.map(h => <th key={h}>{h}</th>)}</tr></thead><tbody>{children}</tbody></table></div>
}

export default function AdminMonitoringPage({ user }: { user?: { is_admin: boolean } | null }) {
  const [hours, setHours] = useState(24)
  const [data, setData] = useState<MonitoringResponse | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [refresh, setRefresh] = useState(0)
  const [beforeID, setBeforeID] = useState<number | undefined>()
  const generation = useRef(0)
  const isAdmin = !!user?.is_admin

  useEffect(() => {
    if (!isAdmin) { setData(null); return }
    let controller: AbortController | undefined
    let active = true
    const load = async () => {
      controller?.abort()
      controller = new AbortController()
      const request = ++generation.current
      setLoading(true)
      try {
        const result = await getAdminMonitoring(hours, beforeID, controller.signal)
        if (!active || request !== generation.current) return
        setData(result)
        setError('')
      } catch {
        if (!active || request !== generation.current || controller.signal.aborted) return
        setData(null)
        setError('监控数据暂不可用，请稍后重试')
      } finally {
        if (active && request === generation.current) setLoading(false)
      }
    }
    setData(null)
    setError('')
    void load()
    const timer = window.setInterval(() => { if (!document.hidden) void load() }, 60_000)
    return () => { active = false; generation.current++; controller?.abort(); window.clearInterval(timer) }
  }, [isAdmin, hours, beforeID, refresh])

  if (!isAdmin) return <div className="card">仅管理员可访问运行监控</div>
  const hasEvents = data && data.status !== 'no_data'
  const attempts = data?.registration.attempts || 0
  const captchaTotal = data ? data.captcha.success + data.captcha.rejected + data.captcha.unavailable : 0
  const trendMax = Math.max(1, ...(data?.time_series || []).map(p => p.registration_success + p.registration_failed + p.captcha_rejected + p.captcha_unavailable + p.limit_denied))

  return <div className="admin-monitoring">
    <header className="monitor-heading">
      <div><h2>运行监控</h2><p className="text-muted">注册、验证码、限流、积压与成本风险</p></div>
      <div className="monitor-controls">
        <label>统计范围 <select aria-label="统计范围" value={hours} onChange={e => { setHours(Number(e.target.value)); setBeforeID(undefined) }}>
          <option value={1}>最近 1 小时</option><option value={24}>最近 24 小时</option><option value={168}>最近 7 天</option>
        </select></label>
        <button disabled={loading} onClick={() => setRefresh(v => v + 1)}>刷新</button>
      </div>
    </header>
    {loading && <p role="status">正在更新监控数据…</p>}
    {error && <p role="alert" className="monitor-error">{error}</p>}
    {data && <>
      <p className="text-muted text-sm">更新时间：{when(data.generated_at)} · 页面可见时每 60 秒刷新</p>
      <p className="text-muted text-sm">采集开始：{when(data.collection_available_since)} · 事件保留 {data.retention_days} 天 · 采集最多延迟 60 秒</p>
      {data.status !== 'available' && <div className="monitor-notice">{data.status === 'no_data' ? '尚无事件数据，不能据此判断历史无故障。' : '当前时间段仅覆盖部分采集历史。'}</div>}
      {data.collection_dropped > 0 && <div role="alert" className="monitor-notice">采集发生丢弃：{number(data.collection_dropped)} 条，统计可能不完整。</div>}
      <section className="card monitor-section" aria-labelledby="monitor-alerts"><h3 id="monitor-alerts">当前异常</h3>
        {!data.alerts.length ? <p className="text-muted">{hasEvents ? '当前未触发已配置告警' : '尚无事件数据；当前队列与额度单独核对。'}</p> : <ul className="monitor-alerts">{data.alerts.map(a => <li key={a.code} className={a.severity}>
          <strong>{a.severity === 'critical' ? '严重' : '提醒'} · {a.message}</strong><span>当前值 {alertValue(a.code, a.value)} ／ 触发阈值 {alertValue(a.code, a.threshold)}</span>
        </li>)}</ul>}
      </section>
      <div className="monitor-cards">
        <section className="card"><h3>注册</h3><strong className="monitor-value">{hasEvents ? number(data.registration.success) : '—'}</strong><p>成功／尝试 {hasEvents ? number(attempts) : '—'}</p><small>失败率 {attempts ? percent(data.registration.failed / attempts) : '尚无数据'}</small></section>
        <section className="card"><h3>验证码服务故障</h3><strong className="monitor-value">{hasEvents ? number(data.captcha.unavailable) : '—'}</strong><p>未通过验证 {hasEvents ? number(data.captcha.rejected) : '—'}</p><small>服务故障率 {captchaTotal ? percent(data.captcha.unavailable / captchaTotal) : '尚无数据'}</small></section>
        <section className="card"><h3>限流拦截</h3><strong className="monitor-value">{hasEvents ? number(data.limit_total) : '—'}</strong><p>所选时间段内</p><small>包括认证和任务额度限制</small></section>
        <section className="card"><h3>今日估算成本</h3><strong className="monitor-value monitor-money">{amount(data.cost.estimated_today)}</strong><p>仅配置单价的任务尝试</p><small>UTC 日界线 · 非真实账单</small></section>
      </div>
      <section className="card monitor-section"><h3>事件趋势</h3><p className="text-muted text-sm">注册结果、验证码拒绝／故障、限流事件；同一请求可能产生多类事件。</p>
        {!data.time_series.length ? <p>尚无数据</p> : <>
          <div className="monitor-trend" role="img" aria-label="事件数量趋势，详细数据见下方表格">{data.time_series.map(p => {
            const parts = [p.registration_success, p.registration_failed, p.captcha_rejected, p.captcha_unavailable, p.limit_denied]
            return <div className="monitor-bar" key={p.at} title={`${when(p.at)}：${parts.reduce((a,b) => a+b,0)} 个事件`}>{parts.map((n,i) => <span key={i} className={`series-${i}`} style={{ height: `${100*n/trendMax}%` }} />)}</div>
          })}</div>
          <p className="monitor-legend">{['注册成功', '注册失败', '验证码未通过', '验证码故障', '限流'].map((name, i) => <span key={name}><i className={`series-${i}`} />{name}</span>)}</p>
          <details><summary>查看趋势明细</summary><Table headers={['时间', '注册成功', '注册失败', '验证码未通过', '验证码故障', '限流']}>{data.time_series.map(p => <tr key={p.at}><td>{when(p.at)}</td><td>{p.registration_success}</td><td>{p.registration_failed}</td><td>{p.captcha_rejected}</td><td>{p.captcha_unavailable}</td><td>{p.limit_denied}</td></tr>)}</Table></details>
        </>}
      </section>
      <section className="card monitor-section"><h3>故障与限流分类</h3>{!data.groups.length ? <p className="text-muted">所选时段尚无记录</p> : <Table headers={['类型', '原因', '任务', '账号', '次数']}>{data.groups.map((g,i) => <tr key={i}><td>{label(g.kind)}</td><td>{label(g.reason)}</td><td>{label(g.task_type)}</td><td>{account(g.user_id)}</td><td>{number(g.count)}</td></tr>)}</Table>}</section>
      <section className="card monitor-section"><h3>当前任务积压</h3><p className="text-muted text-sm">实时快照，不随统计范围切换。</p>
        {data.queues.map(q => <div className="monitor-queue" key={q.name}><h4>{label(q.name)}</h4>{q.status === 'unavailable' ? <p>数据不可用</p> : <p>等待 {number(q.waiting)} · 执行中 {number(q.running)} · 失败 {number(q.failed)} · 过期 {number(q.expired)}</p>}<p>最老等待 {q.waiting ? `${Math.ceil(q.oldest_wait_seconds / 60)} 分钟` : '—'}</p><small className="text-muted">{q.note} · {when(q.snapshot_at)}</small></div>)}
      </section>
      <section className="card monitor-section"><h3>任务额度与成本</h3><p>额度日期：{data.cost.utc_day}（UTC） · 每日金额告警：{data.cost.daily_alert_budget == null ? '未配置' : amount(data.cost.daily_alert_budget)}</p><p className="text-muted text-sm">{data.cost.ledger_retention_note} 调用量按任务尝试统计，可能包含失败尝试；金额为估算，未包含未配置单价的任务。</p>
        <Table headers={['任务', '涉及 UTC 日期调用量', '今日额度', '已用比例', '估算单价/次', '涉及 UTC 日期估算']}>{data.cost.by_task.map(t => <tr key={t.task_type}><td>{label(t.task_type)}</td><td>{number(t.attempts)}</td><td>{number(t.today_attempts)} / {number(t.global_daily_limit)}</td><td>{percent(t.usage_ratio)}</td><td>{t.unit_price == null ? '未配置' : `¥${t.unit_price}`}</td><td>{amount(t.estimated_cost)}</td></tr>)}</Table>
        <h4>用户消耗排行（窗口涉及的完整 UTC 日期）</h4>{!data.cost.by_user.length ? <p className="text-muted">尚无数据</p> : <Table headers={['账号', '任务', '调用量']}>{data.cost.by_user.map(t => <tr key={`${t.user_id}:${t.task_type}`}><td>{account(t.user_id)}</td><td>{label(t.task_type)}</td><td>{number(t.attempts)}</td></tr>)}</Table>}
      </section>
      <section className="card monitor-section"><h3>最近事件</h3>{!data.recent_events.length ? <p className="text-muted">尚无数据</p> : <Table headers={['时间', '类型', '原因', '任务', '账号', '次数', '可恢复时间']}>{data.recent_events.map(e => <tr key={e.id}><td>{when(e.at)}</td><td>{label(e.kind)}</td><td>{label(e.reason)}</td><td>{label(e.task_type)}</td><td>{account(e.user_id)}</td><td>{number(e.count)}</td><td>{e.retry_at ? when(e.retry_at) : '未确定'}</td></tr>)}</Table>}
        <div className="monitor-controls">{beforeID != null && <button disabled={loading} onClick={() => setBeforeID(undefined)}>返回最新事件</button>}{data.next_before_id != null && <button disabled={loading} onClick={() => setBeforeID(data.next_before_id!)}>更早事件</button>}</div>
      </section>
      <p className="text-muted text-sm">验证码配置到期时间：{data.captcha_expires_at ? when(data.captcha_expires_at) : '未配置'} · 此处不代表腾讯云实时余量或账单。</p>
    </>}
  </div>
}
