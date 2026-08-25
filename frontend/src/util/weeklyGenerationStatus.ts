import type { WeeklyDigest } from '../api/client'

const pendingMessage = '周报生成中，稍后刷新…'

function formatBeijingHour(value: string): string | null {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null

  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(date)
  const get = (type: Intl.DateTimeFormatPartTypes) => parts.find(part => part.type === type)?.value
  const year = get('year')
  const month = get('month')
  const day = get('day')
  const hour = get('hour')
  const minute = get('minute')
  return year && month && day && hour && minute ? `${year}-${month}-${day} ${hour}:${minute}` : null
}

export function weeklyEmptyStateMessage(
  digest: Pick<WeeklyDigest, 'pending' | 'generation_status' | 'estimated_generation_at'>,
): string | null {
  switch (digest.generation_status) {
    case 'scheduled': {
      const time = digest.estimated_generation_at && formatBeijingHour(digest.estimated_generation_at)
      return time ? `预计于 ${time}（北京时间）开始生成` : pendingMessage
    }
    case 'pending':
      return pendingMessage
    case 'not_planned':
      return '该周报已过自动生成范围，不再生成。'
    case 'ready':
      return null
    default:
      return digest.pending ? pendingMessage : null
  }
}
