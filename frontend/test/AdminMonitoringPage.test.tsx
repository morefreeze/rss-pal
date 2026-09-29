import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminMonitoringPage from '../src/pages/AdminMonitoringPage'

const api = vi.hoisted(() => ({ getAdminMonitoring: vi.fn() }))
vi.mock('../src/api/monitoring', () => api)

describe('管理员运行监控', () => {
  beforeEach(() => vi.clearAllMocks())

  it('普通用户不能请求管理员统计', () => {
    render(<AdminMonitoringPage user={{ is_admin: false }} />)
    expect(screen.getByText('仅管理员可访问运行监控')).toBeTruthy()
    expect(api.getAdminMonitoring).not.toHaveBeenCalled()
  })

  it('故障显示不可用且可重试，不显示健康零值', async () => {
    api.getAdminMonitoring.mockRejectedValue(new Error('unavailable'))
    render(<AdminMonitoringPage user={{ is_admin: true }} />)
    await screen.findByText('监控数据暂不可用，请稍后重试')
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(api.getAdminMonitoring).toHaveBeenCalledTimes(2))
    expect(screen.queryByText('当前无异常')).toBeNull()
  })
})

const snapshot = {
  generated_at: '2026-09-16T10:00:00Z', collection_available_since: '2026-09-16T08:00:00Z',
  window_start: '2026-09-15T10:00:00Z', window_end: '2026-09-16T10:00:00Z', hours: 24,
  status: 'no_data', retention_days: 30, registration: { attempts: 0, success: 0, failed: 0 },
  captcha: { success: 0, rejected: 0, unavailable: 0 }, limit_total: 0, time_series: [], groups: [],
  queues: [{ name: 'summary', status: 'available', waiting: 4, running: null, failed: null, expired: null, oldest_wait_seconds: 2400, snapshot_at: '2026-09-16T10:00:00Z', note: '仅计算启用订阅的可执行任务' }],
  cost: { utc_day: '2026-09-16', ledger_retention_note: '每日账本按 UTC 统计', estimate_status: 'not_configured', estimated_today: null, daily_alert_budget: null, currency: 'CNY', by_task: [], by_user: [] },
  alerts: [{ code: 'summary_wait', severity: 'warning', message: '摘要等待超过 30 分钟', value: 2400, threshold: 1800 }],
  recent_events: [], next_before_id: null, captcha_expires_at: null, collection_dropped: 0,
}

it('展示积压告警、未配置成本和采集起点，历史无数据不显示零故障', async () => {
  api.getAdminMonitoring.mockResolvedValue(snapshot)
  render(<AdminMonitoringPage user={{ is_admin: true }} />)
  await screen.findByText('提醒 · 摘要等待超过 30 分钟')
  expect(screen.getByText('暂无可计费用量')).toBeTruthy()
  expect(screen.getByText('尚无事件数据，不能据此判断历史无故障。')).toBeTruthy()
  expect(screen.getByText(/最老等待 40 分钟/)).toBeTruthy()
  expect(screen.getByText(/采集开始/)).toBeTruthy()
})

it('切换窗口后丢弃旧请求结果', async () => {
  let resolveOld!: (v: unknown) => void
  api.getAdminMonitoring.mockReturnValueOnce(new Promise(r => { resolveOld = r })).mockResolvedValue({ ...snapshot, alerts: [] })
  render(<AdminMonitoringPage user={{ is_admin: true }} />)
  fireEvent.change(screen.getByLabelText('统计范围'), { target: { value: '168' } })
  await screen.findByText('尚无事件数据，不能据此判断历史无故障。')
  resolveOld(snapshot)
  await waitFor(() => expect(api.getAdminMonitoring).toHaveBeenLastCalledWith(168, undefined, expect.any(AbortSignal)))
  expect(screen.queryByText('提醒 · 摘要等待超过 30 分钟')).toBeNull()
})

it('更早事件采用游标且切换范围重置游标', async () => {
  api.getAdminMonitoring.mockResolvedValue({ ...snapshot, next_before_id: 42 })
  render(<AdminMonitoringPage user={{ is_admin: true }} />)
  fireEvent.click(await screen.findByRole('button', { name: '更早事件' }))
  await waitFor(() => expect(api.getAdminMonitoring).toHaveBeenLastCalledWith(24, 42, expect.any(AbortSignal)))
  fireEvent.change(screen.getByLabelText('统计范围'), { target: { value: '1' } })
  await waitFor(() => expect(api.getAdminMonitoring).toHaveBeenLastCalledWith(1, undefined, expect.any(AbortSignal)))
})


it('自动刷新只在页面可见时发起，卸载后停止', async () => {
  vi.useFakeTimers()
  const hidden = vi.spyOn(document, 'hidden', 'get')
  api.getAdminMonitoring.mockClear().mockResolvedValue(snapshot)
  try {
    const view = render(<AdminMonitoringPage user={{ is_admin: true }} />)
    await act(async () => { await Promise.resolve() })
    hidden.mockReturnValue(true)
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(api.getAdminMonitoring).toHaveBeenCalledTimes(1)
    hidden.mockReturnValue(false)
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(api.getAdminMonitoring).toHaveBeenCalledTimes(2)
    view.unmount()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.getAdminMonitoring).toHaveBeenCalledTimes(2)
  } finally { hidden.mockRestore(); vi.useRealTimers() }
})

it('按 token 展示美元估算，缺失 usage 明确提示', async () => {
  api.getAdminMonitoring.mockResolvedValue({ ...snapshot, token_cost: {
    currency: 'USD', collection_since: '2026-09-24T00:00:00Z', estimated_today: 0.202, today_missing: 1,
    rates: [{provider:'api.z.ai',model:'glm-5.3-flash',input:.15,cached:.03,output:.5}],
    by_model: [{provider:'api.z.ai',model:'glm-5.3-flash',calls:2,missing_usage:1,unpriced_calls:0,input_tokens:1000000,cached_tokens:400000,output_tokens:200000,cost_usd:.202}],
  } })
  render(<AdminMonitoringPage user={{is_admin:true}} />)
  await screen.findByText('今日 token 估算成本')
  expect(screen.getAllByText('$0.202000').length).toBe(2)
  expect(screen.getByText(/今日 1 次请求缺少用量或单价/)).toBeTruthy()
  expect(screen.getByText('单价（USD / 百万 token）')).toBeTruthy()
  expect(screen.queryByText('估算单价/次')).toBeNull()
})
