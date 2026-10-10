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
  global_concurrent: '全局并发限制', concurrency: '并发限制', ai: 'AI 调用（旧混合池）', ai_auto: '自动 AI 调用', ai_manual: '手动 AI 调用',
  fetch: '抓取', interactive: '交互任务', capture: '网摘', pdf: 'PDF', subscribe: '订阅',
  subscription_fetch: '订阅抓取', background_fetch: '内容补抓', background_ocr: '后台 OCR', summary: '摘要',
  explore: '探索', explore_fetch: '探索抓取', explore_validation: '探索验证',
}
const label = (key: string) => labels[key] || key || '—'
const when = (value?: string | null) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '尚无数据'
const number = (value?: number | null) => value == null ? '不可用' : value.toLocaleString('zh-CN')
const percent = (value: number) => `${(value * 100).toFixed(1)}%`
const amount = (value: number | null) => value == null ? '未配置单价' : `¥${value.toFixed(2)}`
const usd = (value?: number | null) => value == null ? '暂无可计费用量' : `$${value.toFixed(6)}`
const account = (id: number) => id > 0 ? `用户 #${id}` : '系统／未登录'
function alertValue(code: string, value: number): string {
  if (code === 'captcha_unavailable' || code.startsWith('quota_')) return percent(value)
  if (code.startsWith('queue_') || code === 'explore_stalled') return `${Math.ceil(value / 60)} 分钟`
  if (code.includes('cost')) return amount(value)
  return number(value)
}
function estimateDuration(seconds: number) {
  const minutes = Math.max(1, Math.ceil(seconds / 60))
  if (minutes < 60) return `${minutes} 分钟`
  const hours = Math.ceil(minutes / 60)
  if (hours < 24) return `${hours} 小时`
  return `${Math.floor(hours / 24)} 天${hours % 24 ? ` ${hours % 24} 小时` : ''}`
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
      {!!data.quota_exhaustions?.length && <section className="card monitor-section"><h3>额度等待恢复</h3><p className="text-muted text-sm">日额度耗尽，恢复后可继续执行；重复被拦截的次数不代表新的故障。</p>{data.quota_exhaustions.map(q => <div className="monitor-queue" key={`${q.task_type}:${q.user_id}:${q.reason}`}><p>{label(q.task_type)} · {q.reason === 'global_daily' ? '全站' : account(q.user_id)} · {number(q.used)} / {number(q.limit)}</p><small>恢复时间：{when(q.retry_at)}</small></div>)}</section>}
      <div className="monitor-cards">
        <section className="card"><h3>注册</h3><strong className="monitor-value">{hasEvents ? number(data.registration.success) : '—'}</strong><p>成功／尝试 {hasEvents ? number(attempts) : '—'}</p><small>失败率 {attempts ? percent(data.registration.failed / attempts) : '尚无数据'}</small></section>
        <section className="card"><h3>验证码服务故障</h3><strong className="monitor-value">{hasEvents ? number(data.captcha.unavailable) : '—'}</strong><p>未通过验证 {hasEvents ? number(data.captcha.rejected) : '—'}</p><small>服务故障率 {captchaTotal ? percent(data.captcha.unavailable / captchaTotal) : '尚无数据'}</small></section>
        <section className="card"><h3>限流拦截</h3><strong className="monitor-value">{hasEvents ? number(data.limit_total) : '—'}</strong><p>所选时间段内</p><small>包括认证和任务额度限制</small></section>
        <section className="card"><h3>今日 token 估算成本</h3><strong className="monitor-value monitor-money">{usd(data.token_cost?.estimated_today)}</strong><p>USD · 按实际 token 与公开单价估算</p><small>UTC 日界线 · 非 Coding 套餐账单</small>{!!data.token_cost?.today_missing && <p>今日 {number(data.token_cost.today_missing)} 次请求缺少用量或单价，合计不完整。</p>}</section>
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
        {data.explore_health && <div className="monitor-queue"><h4>探索处理进度</h4><p>{data.explore_health.status === 'healthy' ? '探索处理正常' : data.explore_health.status === 'expired' ? '有执行租约过期，等待恢复' : '探索处理停滞，请检查 worker'}</p><small className="text-muted">最近处理进展：{when(data.explore_health.last_progress_at)} · 有到期任务时每分钟检查，每批完成后短暂休息；连续 {Math.ceil((data.explore_health.stall_threshold_seconds ?? 1800) / 60)} 分钟无处理进展才告警。</small></div>}
        {data.explore_estimate && <div className="monitor-queue"><h4>探索队列预估处理时长</h4><p>{data.explore_estimate.status === 'estimated' ? `约 ${estimateDuration(data.explore_estimate.seconds ?? 0)} · 预计 ${when(data.explore_estimate.completion_at)} 完成首轮处理（${data.explore_estimate.batches} 批）` : data.explore_estimate.status === 'empty' ? '当前无可执行积压' : data.explore_estimate.status === 'busy' || data.explore_estimate.status === 'unavailable' ? '任务停滞或等待恢复，暂无法估算' : '近期样本不足，暂无法估算'}</p><small className="text-muted">合计探索抓取与关联探索的可执行及执行中任务，按最近 {data.explore_estimate.sample_count} 批处理速度估算，包含批次间短暂等待。假设处理持续正常；不含退避等待、新增任务和后续重试，实际耗时可能变化。</small></div>}
        {data.explore_source_states && <div className="monitor-queue"><h4>探索源状态</h4><p>正常 / 待验证 {number(data.explore_source_states.active)} · 暂时不可访问 / 等待重试 {number(data.explore_source_states.retry_wait)} · 重试耗尽 {number(data.explore_source_states.retry_exhausted)} · 确定不可用 {number(data.explore_source_states.unavailable)} · 不符合收录条件 {number(data.explore_source_states.ineligible)}</p><small className="text-muted">按源去重统计。最多重试 6 次：1 小时、4 小时、16 小时、2 天、8 天、32 天，间隔随机偏移 ±10%。停止状态保留记录，不再自动尝试。</small></div>}
        {data.queues.map(q => {
          const exploration = q.name === 'explore_fetch_queue' || q.name === 'explore_related_tasks'
          return <div className="monitor-queue" key={q.name}><h4>{label(q.name)}</h4>{q.status === 'partial' && <small className="text-muted">状态信息不完整，不能仅凭等待时长判断故障。</small>}{q.status === 'unavailable' ? <p>数据不可用</p> : exploration ? <p>可执行 {number(q.waiting)} · 执行中 {number(q.running)} · 退避等待 {number(q.deferred)} · 待恢复 {number(q.expired)}</p> : <p>等待 {number(q.waiting)} · 执行中 {number(q.running)}</p>}
            {exploration ? <><details><summary>历史记录详情</summary><p>本轮最长排队 {q.waiting && q.ready_wait_seconds != null ? `${Math.ceil(q.ready_wait_seconds / 60)} 分钟` : '—'}<small className="text-muted"> · 从本次可执行时间计起，不等于预计剩余时间</small></p><p>最老待执行记录年龄 {q.waiting ? `${Math.ceil(q.oldest_wait_seconds / 86400)} 天` : '—'} · 历史终止 {number(q.failed)}</p><small>记录年龄包含退避及历次尝试，不用于判断当前是否停滞。</small></details></> : <p>最老等待 {q.waiting ? `${Math.ceil(q.oldest_wait_seconds / 60)} 分钟` : '—'}</p>}
            <small className="text-muted">{q.note} · {when(q.snapshot_at)}</small></div>
        })}
      </section>
      <section className="card monitor-section"><h3>AI token 用量与成本</h3>
        <p className="text-muted text-sm">按所选时间段统计。输入 token 包含缓存命中；费用 =（输入 − 缓存）× 输入单价 + 缓存 × 缓存单价 + 输出 × 输出单价，再除以一百万。历史未采集的用量无法补算。</p>
        <p>Token 统计起点：{when(data.token_cost?.collection_since)} · 币种 USD · API 按量价值估算，不代表 Coding 套餐实际扣费。</p>
        <h4>单价（USD / 百万 token）</h4>
        <Table headers={['服务商', '模型', '输入', '缓存输入', '输出']}>{data.token_cost?.rates.map(r => <tr key={`${r.provider}:${r.model}`}><td>{r.provider}</td><td>{r.model}</td><td>${r.input}</td><td>${r.cached}</td><td>${r.output}</td></tr>)}</Table>
        <p className="text-muted text-sm">单价核对日期：2026-09-24 · <a href="https://docs.z.ai/guides/overview/pricing" target="_blank" rel="noreferrer">Z.AI 官方价格</a> · 未配置的模型不计入金额。</p>
        {!data.token_cost?.by_model.length ? <p>所选时段尚无 token 用量记录</p> : <Table headers={['模型 / 服务商', '请求次数', '输入 token', '缓存 token', '输出 token', '缺少用量 / 单价', '估算费用（USD）']}>{data.token_cost.by_model.map(m => <tr key={`${m.provider}:${m.model}`}><td>{m.model}<small> · {m.provider}</small></td><td>{number(m.calls)}</td><td>{number(m.input_tokens)}</td><td>{number(m.cached_tokens)}</td><td>{number(m.output_tokens)}</td><td>{number(m.missing_usage)} / {number(m.unpriced_calls)}</td><td>{usd(m.cost_usd)}{(m.missing_usage > 0 || m.unpriced_calls > 0) && <small> · 不完整</small>}</td></tr>)}</Table>}
      </section>
      <section className="card monitor-section"><h3>任务额度</h3><p>额度日期：{data.cost.utc_day}（UTC）</p><p className="text-muted text-sm">{data.cost.ledger_retention_note} 调用量按任务尝试统计，可能包含失败尝试。</p>
        <Table headers={['任务', '涉及 UTC 日期调用量', '今日额度', '已用比例']}>{data.cost.by_task.map(t => <tr key={t.task_type}><td>{label(t.task_type)}</td><td>{number(t.attempts)}</td><td>{t.global_daily_limit > 0 ? `${number(t.today_attempts)} / ${number(t.global_daily_limit)}` : '历史记录，不再计额'}</td><td>{t.global_daily_limit > 0 ? percent(t.usage_ratio) : '—'}</td></tr>)}</Table>
        <h4>用户消耗排行（窗口涉及的完整 UTC 日期）</h4>{!data.cost.by_user.length ? <p className="text-muted">尚无数据</p> : <Table headers={['账号', '任务', '调用量']}>{data.cost.by_user.map(t => <tr key={`${t.user_id}:${t.task_type}`}><td>{account(t.user_id)}</td><td>{label(t.task_type)}</td><td>{number(t.attempts)}</td></tr>)}</Table>}
      </section>
      <section className="card monitor-section"><h3>最近事件</h3>{!data.recent_events.length ? <p className="text-muted">尚无数据</p> : <Table headers={['时间', '类型', '原因', '任务', '账号', '次数', '可恢复时间']}>{data.recent_events.map(e => <tr key={e.id}><td>{when(e.at)}</td><td>{label(e.kind)}</td><td>{label(e.reason)}</td><td>{label(e.task_type)}</td><td>{account(e.user_id)}</td><td>{number(e.count)}</td><td>{e.retry_at ? when(e.retry_at) : '未确定'}</td></tr>)}</Table>}
        <div className="monitor-controls">{beforeID != null && <button disabled={loading} onClick={() => setBeforeID(undefined)}>返回最新事件</button>}{data.next_before_id != null && <button disabled={loading} onClick={() => setBeforeID(data.next_before_id!)}>更早事件</button>}</div>
      </section>
      <p className="text-muted text-sm">验证码配置到期时间：{data.captcha_expires_at ? when(data.captcha_expires_at) : '未配置'} · 此处不代表腾讯云实时余量或账单。</p>
    </>}
  </div>
}
