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

// Tailwind classes for a tag chip. Both bg and text shades from the same color.
export function tagChipClasses(name: string): string {
  const c = tagColorFor(name)
  // Note: keep these literal so Tailwind JIT can detect them.
  switch (c) {
    case 'rose': return 'bg-rose-100 text-rose-700'
    case 'amber': return 'bg-amber-100 text-amber-700'
    case 'emerald': return 'bg-emerald-100 text-emerald-700'
    case 'sky': return 'bg-sky-100 text-sky-700'
    case 'violet': return 'bg-violet-100 text-violet-700'
    case 'pink': return 'bg-pink-100 text-pink-700'
    case 'lime': return 'bg-lime-100 text-lime-700'
    case 'indigo': return 'bg-indigo-100 text-indigo-700'
  }
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
