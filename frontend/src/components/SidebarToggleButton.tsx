import UiIcon from './UiIcon'
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
      <UiIcon name="panelLeft" />
    </button>
  )
}
