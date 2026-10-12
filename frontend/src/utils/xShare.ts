import Graphemer from 'graphemer'

export const X_MAX_WEIGHT = 140
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
const fallbackSegmenter = new Graphemer()

export type XPostInput = {
  title: string
  summaryBrief?: string
  summaryDetailed?: string
  shareURL: string
  content?: string
}

export function fallbackGraphemes(text: string): string[] {
  return fallbackSegmenter.splitGraphemes(text)
}

function graphemes(text: string): string[] {
  return segmenter
    ? Array.from(segmenter.segment(text), part => part.segment)
    : fallbackGraphemes(text)
}

function graphemeWeight(grapheme: string): number {
  return /^[\x00-\x7f]+$/u.test(grapheme) ? 1 : 2
}

function plainWeightedLength(text: string): number {
  return graphemes(text).reduce((total, grapheme) => total + graphemeWeight(grapheme), 0)
}

export function plainSocialText(markdown: string): string {
  return markdown
    .replace(/<(script|style)\b[^>]*>[\s\S]*?<\/\1>/giu, ' ')
    .replace(/!\[([^\]]*)\]\([^)]*\)/gu, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/gu, '$1')
    .replace(/```[^\n]*\n?/gu, ' ')
    .replace(/`([^`]*)`/gu, '$1')
    .replace(/^\s{0,3}(?:#{1,6}\s+|>\s*|[-+*]\s+|\d+[.)]\s+)/gmu, '')
    .replace(/<[^>]+>/gu, ' ')
    .replace(/&(?:amp|lt|gt|quot|apos|nbsp);/gu, entity => ({'&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"', '&apos;': "'", '&nbsp;': ' '}[entity] ?? entity))
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
  let budget = X_MAX_WEIGHT
  let text = composePost(input, budget)
  // Other destinations count the literal URL rather than X's shortened URL.
  while (Array.from(text).length > 140 && budget > X_URL_WEIGHT + 2) {
    budget = Math.max(X_URL_WEIGHT + 2, budget - (Array.from(text).length - 140))
    text = composePost(input, budget)
  }
  return text
}

function composePost(input: XPostInput, budget: number): string {
  const shareURL = input.shareURL.trim()
  const rawTitle = plainSocialText(input.title) || '分享文章'
  const rawSummary = plainSocialText(input.summaryBrief ?? '')
    || plainSocialText(input.summaryDetailed ?? '')
    || plainSocialText(input.content ?? '')

  if (budget <= X_URL_WEIGHT + 2) return shareURL

  if (rawSummary) {
    const contentBudget = Math.max(0, budget - X_URL_WEIGHT - 4)
    const firstSummaryGrapheme = graphemes(rawSummary)[0]
    const minimumSummaryBudget = Math.min(
      plainWeightedLength(rawSummary),
      graphemeWeight(firstSummaryGrapheme) + graphemeWeight('…'),
    )
    const title = truncateXText(rawTitle, Math.max(0, contentBudget - minimumSummaryBudget))
    const summaryBudget = contentBudget - xWeightedLength(title)
    if (summaryBudget > 0) {
      const summary = truncateXText(rawSummary, summaryBudget)
      if (summary) return `${title}\n\n${summary}\n\n${shareURL}`
    }
  }

  const titleBudget = Math.max(0, budget - X_URL_WEIGHT - 2)
  const title = truncateXText(rawTitle, titleBudget)
  return `${title}\n\n${shareURL}`
}

export function buildXIntentURL(postText: string): string {
  const intent = new URL('https://x.com/intent/post')
  intent.searchParams.set('text', postText)
  return intent.toString()
}
