import { api } from './client'

export interface MonitoringEvent {
  id: number; at: string; kind: string; reason: string; task_type: string
  user_id: number; count: number; retry_at?: string
}
export interface MonitoringResponse {
  generated_at: string; collection_available_since: string; window_start: string; window_end: string
  hours: number; status: 'available' | 'partial' | 'no_data'; retention_days: number
  registration: { attempts: number; success: number; failed: number }
  captcha: { success: number; rejected: number; unavailable: number }
  limit_total: number
  time_series: { at: string; registration_success: number; registration_failed: number; captcha_rejected: number; captcha_unavailable: number; limit_denied: number }[]
  groups: { kind: string; reason: string; task_type: string; user_id: number; count: number }[]
  queues: { name: string; status: string; waiting: number; running: number | null; failed: number | null; expired: number | null; oldest_wait_seconds: number; snapshot_at: string; note: string }[]
  cost: {
    utc_day: string; ledger_retention_note: string; estimate_status: 'configured' | 'partial' | 'not_configured'
    estimated_today: number | null; daily_alert_budget: number | null; currency: string
    by_task: { task_type: string; attempts: number; today_attempts: number; global_daily_limit: number; usage_ratio: number; unit_price: number | null; estimated_cost: number | null }[]
    by_user: { user_id: number; task_type: string; attempts: number }[]
  }
  alerts: { code: string; severity: 'warning' | 'critical'; message: string; value: number; threshold: number }[]
  recent_events: MonitoringEvent[]; next_before_id: number | null
  captcha_expires_at: string | null; collection_dropped: number
}
export const getAdminMonitoring = (hours: number, beforeID?: number, signal?: AbortSignal) =>
  api.get<MonitoringResponse>('/admin/monitoring', { params: { hours, before_id: beforeID, limit: 50 }, signal }).then(r => r.data)
