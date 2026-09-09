export const X_MAX_WEIGHT = 280
export const X_URL_WEIGHT = 23

const URL_PATTERN = /https?:\/\/[^\s]+/giu

type GraphemeSegment = { segment: string }
type GraphemeSegmenter = { segment: (text: string) => Iterable<GraphemeSegment> }
type GraphemeSegmenterConstructor = new (
  locales?: string | string[],
  options?: { granularity: 'grapheme' },
) => GraphemeSegmenter

const Segmenter = (Intl as typeof Intl & { Segmenter?: GraphemeSegmenterConstructor }).Segmenter
const segmenter = Segmenter ? new Segmenter(undefined, { granularity: 'grapheme' }) : null

export type XPostInput = {
  title: string
  summaryBrief?: string
  summaryDetailed?: string
  shareURL: string
}

function graphemes(text: string): string[] {
  return segmenter
    ? Array.from(segmenter.segment(text), part => part.segment)
    : Array.from(text)
}

function graphemeWeight(grapheme: string): number {
  return /^[\x00-\x7f]+$/u.test(grapheme) ? 1 : 2
}

function plainWeightedLength(text: string): number {
  return graphemes(text).reduce((total, grapheme) => total + graphemeWeight(grapheme), 0)
}

export function plainSocialText(markdown: string): string {
  return markdown
    .replace(/!\[([^\]]*)\]\([^)]*\)/gu, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/gu, '$1')
    .replace(/```[^\n]*\n?/gu, ' ')
    .replace(/`([^`]*)`/gu, '$1')
    .replace(/^\s{0,3}(?:#{1,6}\s+|>\s*|[-+*]\s+|\d+[.)]\s+)/gmu, '')
    .replace(/<[^>]+>/gu, ' ')
    .replace(URL_PATTERN, ' ')
    .replace(/[\*_~]+/gu, '')
    .replace(/\s+/gu, ' ')
    .trim()
}

export function xWeightedLength(text: string): number {
  let total = 0
  let cursor = 0
  for (const match of text.matchAll(URL_PATTERN)) {
    const index = match.index ?? cursor
    total += plainWeightedLength(text.slice(cursor, index)) + X_URL_WEIGHT
    cursor = index + match[0].length
  }
  return total + plainWeightedLength(text.slice(cursor))
}

export function truncateXText(text: string, maxWeight: number): string {
  if (maxWeight <= 0) return ''
  if (plainWeightedLength(text) <= maxWeight) return text

  const ellipsis = '…'
  const contentBudget = maxWeight - graphemeWeight(ellipsis)
  if (contentBudget < 0) return ''

  let result = ''
  let used = 0
  for (const grapheme of graphemes(text)) {
    const weight = graphemeWeight(grapheme)
    if (used + weight > contentBudget) break
    result += grapheme
    used += weight
  }
  return `${result.trimEnd()}${ellipsis}`
}

export function buildXPostText(input: XPostInput): string {
  const shareURL = input.shareURL.trim()
  const rawTitle = plainSocialText(input.title) || '分享文章'
  const rawSummary = plainSocialText(input.summaryBrief ?? '')
    || plainSocialText(input.summaryDetailed ?? '')

  const titleBudget = Math.max(0, X_MAX_WEIGHT - X_URL_WEIGHT - 2)
  const title = truncateXText(rawTitle, titleBudget)

  if (rawSummary) {
    const summaryBudget = X_MAX_WEIGHT - xWeightedLength(title) - X_URL_WEIGHT - 4
    if (summaryBudget > 0) {
      const summary = truncateXText(rawSummary, summaryBudget)
      if (summary) return `${title}\n\n${summary}\n\n${shareURL}`
    }
  }

  return `${title}\n\n${shareURL}`
}

export function buildXIntentURL(postText: string): string {
  const intent = new URL('https://x.com/intent/post')
  intent.searchParams.set('text', postText)
  return intent.toString()
}
