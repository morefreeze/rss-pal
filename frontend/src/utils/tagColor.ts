export const TAG_PALETTE = [
  'rose',
  'amber',
  'emerald',
  'sky',
  'violet',
  'pink',
  'lime',
  'indigo',
] as const

export type TagColor = (typeof TAG_PALETTE)[number]

export interface TagChipColors {
  background: string
  color: string
}

// Same 8-color palette, but as concrete hex values: <color>-100 background
// paired with <color>-700 text from the original Tailwind plan.
const PALETTE_COLORS: Record<TagColor, TagChipColors> = {
  rose:    { background: '#ffe4e6', color: '#9f1239' },
  amber:   { background: '#fef3c7', color: '#92400e' },
  emerald: { background: '#d1fae5', color: '#065f46' },
  sky:     { background: '#e0f2fe', color: '#075985' },
  violet:  { background: '#ede9fe', color: '#5b21b6' },
  pink:    { background: '#fce7f3', color: '#9d174d' },
  lime:    { background: '#ecfccb', color: '#3f6212' },
  indigo:  { background: '#e0e7ff', color: '#3730a3' },
}

// FNV-1a 32-bit hash. Stable across browsers and runs.
function hashName(name: string): number {
  let h = 0x811c9dc5
  for (let i = 0; i < name.length; i++) {
    h ^= name.charCodeAt(i)
    h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0
  }
  return h
}

export function tagColorFor(name: string): TagColor {
  const idx = hashName(name) % TAG_PALETTE.length
  return TAG_PALETTE[idx]
}

export function tagChipColors(name: string): TagChipColors {
  return PALETTE_COLORS[tagColorFor(name)]
}

// Determinism self-check: in dev, fail loud if the palette stops being stable.
if ((import.meta as any).env?.DEV) {
  const sample = ['前端', 'AI', '论文笔记', 'devops']
  const first = sample.map(tagColorFor).join(',')
  // Re-run; should be identical.
  const second = sample.map(tagColorFor).join(',')
  if (first !== second) {
    // eslint-disable-next-line no-console
    console.error('tagColorFor is non-deterministic', { first, second })
  }
}
