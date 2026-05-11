import { FeedHealthKPI as KPI } from '../api/client'

const cardStyle: React.CSSProperties = {
  flex: 1,
  padding: '12px 16px',
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 8,
  textAlign: 'center',
}

const numStyle: React.CSSProperties = {
  fontSize: 28,
  fontWeight: 600,
  lineHeight: '36px',
}

const labelStyle: React.CSSProperties = {
  fontSize: 12,
  color: 'var(--fg-muted)',
  marginTop: 4,
}

export default function FeedHealthKPI({ kpi, window }: { kpi: KPI; window: '30d' | '90d' }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12, marginBottom: 16 }}>
      <div style={cardStyle}>
        <div style={numStyle}>{kpi.total_active}</div>
        <div style={labelStyle}>活跃 feed</div>
      </div>
      <div style={cardStyle}>
        <div style={{ ...numStyle, color: '#2a8' }}>{kpi.healthy}</div>
        <div style={labelStyle}>健康</div>
      </div>
      <div style={cardStyle}>
        <div style={{ ...numStyle, color: '#c80' }}>{kpi.dormant}</div>
        <div style={labelStyle}>沉睡</div>
      </div>
      <div style={cardStyle}>
        <div style={numStyle}>{kpi.completed_reads_w}</div>
        <div style={labelStyle}>{window} 完读篇数</div>
      </div>
    </div>
  )
}
