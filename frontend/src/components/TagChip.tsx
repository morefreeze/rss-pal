import { tagChipClasses } from '../utils/tagColor'

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
        onClick={onClick}
        className={
          'inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs ' +
          'bg-slate-100 text-slate-600 ' +
          (onClick ? 'cursor-pointer hover:bg-slate-200' : '')
        }
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
        onClick={onAdopt}
        className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs border border-dashed border-slate-300 text-slate-500 hover:bg-slate-50 hover:border-solid"
      >
        <span>⊕</span>
        <span>{name}</span>
      </button>
    )
  }
  // manual
  return (
    <span className={'inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs ' + tagChipClasses(name)}>
      <span onClick={onClick} className={onClick ? 'cursor-pointer' : undefined}>{name}</span>
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          className="opacity-60 hover:opacity-100"
          aria-label={`移除 ${name}`}
        >
          ✕
        </button>
      )}
    </span>
  )
}
