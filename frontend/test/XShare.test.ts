import { describe, expect, it } from 'vitest'

import {
  buildXIntentURL,
  buildXPostText,
  fallbackGraphemes,
  plainSocialText,
  xWeightedLength,
} from '../src/utils/xShare'

describe('X share post composer', () => {
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
    expect(xWeightedLength(text)).toBeLessThanOrEqual(280)
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
    expect(xWeightedLength(text)).toBeLessThanOrEqual(280)
    expect(text.endsWith('https://rss.example/share/token')).toBe(true)
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

  it('encodes the complete post into an X intent URL', () => {
    const href = buildXIntentURL('ready post')
    const url = new URL(href)

    expect(url.origin + url.pathname).toBe('https://x.com/intent/post')
    expect(url.searchParams.get('text')).toBe('ready post')
  })
})
