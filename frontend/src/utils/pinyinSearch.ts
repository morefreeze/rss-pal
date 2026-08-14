import { pinyin } from 'pinyin-pro'

function normalizeSearchToken(value: string): string {
  return value
    .trim()
    .toLocaleLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, '')
}

export function pinyinSearchIncludes(text: string, query: string): boolean {
  const needle = normalizeSearchToken(query)
  if (!needle) return true
  const source = text.toLocaleLowerCase()
  if (source.includes(query.trim().toLocaleLowerCase())) return true

  const full = pinyin(text, { toneType: 'none', type: 'array' })
    .map(part => normalizeSearchToken(part))
    .join('')
  const initials = pinyin(text, { pattern: 'first', toneType: 'none', type: 'array' })
    .map(part => normalizeSearchToken(part))
    .join('')
  return full.includes(needle) || initials.includes(needle)
}
