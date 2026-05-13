interface Props {
  open: boolean
  onToggle: () => void
}

export default function SidebarToggleButton({ open, onToggle }: Props) {
  return (
    <button
      type="button"
      className="btn-ghost"
      onClick={onToggle}
      title={open ? '收起侧栏 (T)' : '展开侧栏 (T)'}
      aria-label={open ? '收起侧栏' : '展开侧栏'}
      aria-pressed={open}
      style={{ padding: '4px 8px' }}
    >
      <svg
        width="18"
        height="18"
        viewBox="0 0 18 18"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
      >
        <rect x="2" y="2.5" width="14" height="13" rx="2" />
        <line x1="7" y1="2.5" x2="7" y2="15.5" />
        {open && <line x1="3.5" y1="6" x2="5.5" y2="6" />}
        {open && <line x1="3.5" y1="9" x2="5.5" y2="9" />}
      </svg>
    </button>
  )
}
