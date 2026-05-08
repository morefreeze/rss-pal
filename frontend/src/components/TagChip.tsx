import { tagChipColors } from '../utils/tagColor'

type Variant = 'manual' | 'source' | 'suggestion'

interface Props {
  name: string
  variant?: Variant
  onRemove?: () => void
  onAdopt?: () => void
  onClick?: () => void
}

export default function TagChip({ name, variant = 'manual', onRemove, onAdopt, onClick }: Props) {
  if (variant === 'source') {
    return (
      <span
        className="tag-chip tag-chip-source"
        onClick={onClick}
        style={onClick ? { cursor: 'pointer' } : undefined}
      >
        <span>📡</span>
        <span>{name}</span>
      </span>
    )
  }
  if (variant === 'suggestion') {
    return (
      <button
        type="button"
        className="tag-chip tag-chip-suggestion"
        onClick={onAdopt}
      >
        <span>⊕</span>
        <span>{name}</span>
      </button>
    )
  }
  // manual
  const colors = tagChipColors(name)
  return (
    <span
      className="tag-chip"
      style={{ background: colors.background, color: colors.color }}
    >
      <span onClick={onClick} style={onClick ? { cursor: 'pointer' } : undefined}>{name}</span>
      {onRemove && (
        <button
          type="button"
          className="tag-chip-remove"
          onClick={onRemove}
          aria-label={`移除 ${name}`}
        >
          ✕
        </button>
      )}
    </span>
  )
}
