import { Link } from 'react-router-dom'
import { setBriefingLastTab, BriefingTab } from '../api/client'

interface Props {
  current: BriefingTab
}

export default function BriefingTabs({ current }: Props) {
  const onClick = (tab: BriefingTab) => {
    setBriefingLastTab(tab).catch(() => { /* best-effort */ })
  }
  const baseStyle: React.CSSProperties = {
    padding: '8px 16px',
    borderRadius: 8,
    textDecoration: 'none',
    fontWeight: 600,
    fontSize: 14,
  }
  const activeStyle: React.CSSProperties = {
    ...baseStyle,
    background: 'var(--accent, #2563eb)',
    color: '#fff',
  }
  const inactiveStyle: React.CSSProperties = {
    ...baseStyle,
    background: 'transparent',
    color: 'var(--fg)',
    border: '1px solid var(--border)',
  }
  return (
    <div role="tablist" style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
      <Link
        to="/daily"
        role="tab"
        aria-selected={current === 'daily'}
        onClick={() => onClick('daily')}
        style={current === 'daily' ? activeStyle : inactiveStyle}
      >
        日报
      </Link>
      <Link
        to="/weekly"
        role="tab"
        aria-selected={current === 'weekly'}
        onClick={() => onClick('weekly')}
        style={current === 'weekly' ? activeStyle : inactiveStyle}
      >
        周报
      </Link>
    </div>
  )
}
