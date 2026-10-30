import { pinyin } from 'pinyin-pro'

export type SearchMatch = { start: number; end: number }

function literalMatches(text: string, query: string): SearchMatch[] {
  const pattern = new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'giu')
  return Array.from(text.matchAll(pattern), match => ({ start: match.index!, end: match.index! + match[0].length }))
}

export function findSearchMatches(text: string, query: string): SearchMatch[] {
  const needle = query.trim()
  if (!needle) return []
  const direct = literalMatches(text, needle)
  if (direct.length || !/^[a-z0-9]+$/i.test(needle) || !/\p{Script=Han}/u.test(text)) return direct
  const full: string[] = [], initials: string[] = []
  const fullOffsets: SearchMatch[] = [], initialOffsets: SearchMatch[] = []
  let offset = 0
  for (const char of text) {
    const range = { start: offset, end: offset + char.length }
    const token = pinyin(char, { toneType: 'none', type: 'array' }).join('').toLowerCase().replace(/[^\p{L}\p{N}]/gu, '')
    full.push(token)
    initials.push(token[0] || '')
    for (let i = 0; i < token.length; i++) fullOffsets.push(range)
    if (token) initialOffsets.push(range)
    offset += char.length
  }
  for (const [source, positions] of [[full.join(''), fullOffsets], [initials.join(''), initialOffsets]] as const) {
    const matches = literalMatches(source, needle).map(({ start, end }) => ({ start: positions[start].start, end: positions[end - 1].end }))
    if (matches.length) return matches.filter((match, index) => index === 0 || match.start >= matches[index - 1].end)
  }
  return []
}
