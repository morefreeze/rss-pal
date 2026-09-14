import { describe, expect, it } from 'vitest'

import {
  buildXIntentURL,
  buildXPostText,
  fallbackGraphemes,
  plainSocialText,
  truncateXText,
  X_MAX_WEIGHT,
  X_URL_WEIGHT,
  xWeightedLength,
} from '../src/utils/xShare'

describe('X share post composer', () => {
  it('caps composed posts at the product limit of 140 weighted characters', () => {
    expect(X_MAX_WEIGHT).toBe(140)
  })

  it('uses X weights for ASCII, CJK, emoji, and URLs', () => {
    expect(xWeightedLength('ab中😀 https://rss.example/share/token')).toBe(1 + 1 + 2 + 2 + 1 + 23)
  })

  it('prefers the brief summary and converts Markdown to plain text', () => {
    const text = buildXPostText({
      title: 'Article title',
      summaryBrief: '**Brief** [source](https://private.example/feed)',
      summaryDetailed: 'Detailed fallback',
      shareURL: 'https://rss.example/share/token',
    })

    expect(text).toContain('Brief source')
    expect(text).not.toContain('Detailed fallback')
    expect(text).not.toContain('private.example')
    expect(text.endsWith('https://rss.example/share/token')).toBe(true)
  })

  it('falls back to the detailed summary and stays within the weighted limit', () => {
    const text = buildXPostText({
      title: '中文标题',
      summaryBrief: '',
      summaryDetailed: `很长的总结${'内容'.repeat(200)}`,
      shareURL: 'https://rss.example/share/token',
    })

    expect(text).toContain('很长的总结')
    expect(text).toContain('…')
    expect(xWeightedLength(text)).toBeLessThanOrEqual(X_MAX_WEIGHT)
    expect(text.endsWith('https://rss.example/share/token')).toBe(true)
  })

  it('preserves whole emoji graphemes while truncating an extreme title', () => {
    const text = buildXPostText({
      title: '家庭👨‍👩‍👧‍👦'.repeat(100),
      summaryBrief: 'summary',
      shareURL: 'https://rss.example/share/token',
    })

    expect(text).not.toContain('�')
    expect(text).not.toMatch(/[\u200d\ufe0f]…/u)
    expect(xWeightedLength(text)).toBeLessThanOrEqual(X_MAX_WEIGHT)
    expect(text.endsWith('https://rss.example/share/token')).toBe(true)
  })

  it('keeps a maximal complete summary grapheme after an extreme title', () => {
    const summaryBrief = `👨‍👩‍👧‍👦中文摘要${'内容'.repeat(100)}`
    const shareURL = 'https://r.morefreeze.top/Aa0000000000'
    const text = buildXPostText({
      title: '超长标题'.repeat(100),
      summaryBrief,
      shareURL,
    })

    expect(text.endsWith(shareURL)).toBe(true)
    expect(xWeightedLength(text)).toBeLessThanOrEqual(X_MAX_WEIGHT)

    const postWithoutURL = text.slice(0, -`\n\n${shareURL}`.length)
    const [renderedTitle, renderedSummary] = postWithoutURL.split('\n\n')
    expect(renderedSummary).toBeDefined()

    const renderedSummaryGraphemes = fallbackGraphemes(renderedSummary)
    const sourceSummaryGraphemes = fallbackGraphemes(plainSocialText(summaryBrief))
    expect(renderedSummaryGraphemes).toEqual([sourceSummaryGraphemes[0], '…'])

    const consumedSummaryGraphemes = renderedSummaryGraphemes.slice(0, -1)
    expect(consumedSummaryGraphemes).toEqual(
      sourceSummaryGraphemes.slice(0, consumedSummaryGraphemes.length),
    )
    const nextSummaryGrapheme = sourceSummaryGraphemes[consumedSummaryGraphemes.length]
    expect(nextSummaryGrapheme).toBeDefined()

    const oneMoreGrapheme = `${renderedTitle}\n\n${[
      ...consumedSummaryGraphemes,
      nextSummaryGrapheme,
      '…',
    ].join('')}\n\n${shareURL}`
    expect(xWeightedLength(oneMoreGrapheme)).toBeGreaterThan(X_MAX_WEIGHT)
  })

  it('keeps composed graphemes whole when Intl.Segmenter is unavailable', () => {
    expect(fallbackGraphemes('e\u0301 👍🏽 👨‍👩‍👧‍👦 🇨🇳 가')).toEqual([
      'e\u0301',
      ' ',
      '👍🏽',
      ' ',
      '👨‍👩‍👧‍👦',
      ' ',
      '🇨🇳',
      ' ',
      '가',
    ])
  })

  it('removes bare URLs, list syntax, and code markers from social text', () => {
    expect(plainSocialText('- Read `code` at https://private.example/path\n> **Now**')).toBe('Read code at Now')
  })

  it('omits the summary cleanly when no summary is available', () => {
    expect(buildXPostText({
      title: 'Title',
      shareURL: 'https://rss.example/share/token',
    })).toBe('Title\n\nhttps://rss.example/share/token')
  })

  it('keeps the original title-only budget when no summary is available', () => {
    const title = '超长标题'.repeat(100)
    const shareURL = 'https://r.morefreeze.top/Aa0000000000'

    expect(buildXPostText({ title, shareURL })).toBe(
      `${truncateXText(title, X_MAX_WEIGHT - X_URL_WEIGHT - 2)}\n\n${shareURL}`,
    )
  })

  it('maximizes a long Chinese summary while preserving the short share URL', () => {
    const summaryBrief = `这是一个超长中文摘要前缀${'摘要内容'.repeat(100)}`
    const shareURL = 'https://r.morefreeze.top/Aa0000000000'
    const text = buildXPostText({
      title: '中文标题',
      summaryBrief,
      shareURL,
    })

    expect(text).toContain('中文标题')
    expect(text).toContain('这是一个超长中文摘要前缀')
    expect(text.endsWith(shareURL)).toBe(true)
    expect(xWeightedLength(text)).toBeLessThanOrEqual(X_MAX_WEIGHT)

    const postWithoutURL = text.slice(0, -`\n\n${shareURL}`.length)
    const [renderedTitle, renderedSummary] = postWithoutURL.split('\n\n')
    const renderedSummaryGraphemes = fallbackGraphemes(renderedSummary)
    expect(renderedSummaryGraphemes.at(-1)).toBe('…')

    const consumedSummaryGraphemes = renderedSummaryGraphemes.slice(0, -1)
    const sourceSummaryGraphemes = fallbackGraphemes(plainSocialText(summaryBrief))
    expect(consumedSummaryGraphemes).toEqual(
      sourceSummaryGraphemes.slice(0, consumedSummaryGraphemes.length),
    )
    const nextSummaryGrapheme = sourceSummaryGraphemes[consumedSummaryGraphemes.length]
    expect(nextSummaryGrapheme).toBeDefined()

    const oneMoreGrapheme = `${renderedTitle}\n\n${[
      ...consumedSummaryGraphemes,
      nextSummaryGrapheme,
      '…',
    ].join('')}\n\n${shareURL}`
    expect(xWeightedLength(oneMoreGrapheme)).toBeGreaterThan(X_MAX_WEIGHT)
  })

  it('encodes the complete post into an X intent URL', () => {
    const href = buildXIntentURL('ready post')
    const url = new URL(href)

    expect(url.origin + url.pathname).toBe('https://x.com/intent/post')
    expect(url.searchParams.get('text')).toBe('ready post')
  })
})
