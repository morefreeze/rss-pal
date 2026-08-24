import { describe, expect, it } from 'vitest'

import { ARTICLE_ANCHOR_LABEL, ARTICLE_ANCHOR_RE, annotateArticleMarkdown, parseArticleAnchor } from './articleAnchors'

describe('article anchors', () => {
  it('recognizes only canonical internal hrefs and returns their bare DOM IDs', () => {
    expect(ARTICLE_ANCHOR_LABEL).toBe('rss-pal-anchor')
    expect(ARTICLE_ANCHOR_RE.test('#article-section-001')).toBe(true)
    expect(ARTICLE_ANCHOR_RE.test('#article-section-999')).toBe(true)
    expect(ARTICLE_ANCHOR_RE.test('#article-section-1000')).toBe(true)
    expect(parseArticleAnchor('#article-section-001')).toBe('article-section-001')
    expect(parseArticleAnchor('#article-section-1000')).toBe('article-section-1000')

    for (const href of [
      '#article-section-000',
      '#article-section-01',
      '#article-section-1',
      '#article-section-0001',
      '#article-section-001x',
      'https://example.com/#article-section-001',
      '#other-section-001',
    ]) {
      expect(parseArticleAnchor(href)).toBeNull()
    }
  })

  it.each([
    {
      name: 'heading multiline paragraph inline link list and fenced code',
      source: '# Title\n\nA paragraph\nthat continues with [a link](https://example.com).\n\n- first item\n  continuation\n- ![only image](https://example.com/image.png)\n\n```go\nfmt.Println("not an article block")\n```\n\n## End\n',
      expected: '# [rss-pal-anchor](#article-section-001)Title\n\n[rss-pal-anchor](#article-section-002)A paragraph\nthat continues with [a link](https://example.com).\n\n- [rss-pal-anchor](#article-section-003)first item\n  continuation\n- ![only image](https://example.com/image.png)\n\n```go\nfmt.Println("not an article block")\n```\n\n## [rss-pal-anchor](#article-section-004)End\n',
    },
    {
      name: 'blank content',
      source: '\n  \n\t\n',
      expected: '\n  \n\t\n',
    },
    {
      name: 'image only block',
      source: '![cover](https://example.com/cover.png)\n\n![second](https://example.com/second.png)\n',
      expected: '![cover](https://example.com/cover.png)\n\n![second](https://example.com/second.png)\n',
    },
    {
      name: 'link only paragraph is addressable',
      source: '[Read the report](https://example.com/report)\n',
      expected: '[rss-pal-anchor](#article-section-001)[Read the report](https://example.com/report)\n',
    },
    {
      name: 'ordered and nested lists are deterministic',
      source: '1. first\n   1. nested one\n   2. nested two\n2. second\n   - nested bullet\n',
      expected: '1. [rss-pal-anchor](#article-section-001)first\n   1. [rss-pal-anchor](#article-section-002)nested one\n   2. [rss-pal-anchor](#article-section-003)nested two\n2. [rss-pal-anchor](#article-section-004)second\n   - [rss-pal-anchor](#article-section-005)nested bullet\n',
    },
    {
      name: 'blockquote',
      source: '> quoted text\n> continued quote\n\noutside\n',
      expected: '> [rss-pal-anchor](#article-section-001)quoted text\n> continued quote\n\n[rss-pal-anchor](#article-section-002)outside\n',
    },
    {
      name: 'tilde fence',
      source: '~~~markdown\n# not a heading\n~~~\n\nAfter fence\n',
      expected: '~~~markdown\n# not a heading\n~~~\n\n[rss-pal-anchor](#article-section-001)After fence\n',
    },
    {
      name: 'thematic separators',
      source: '---\n\n***\n\n___\n\nText\n',
      expected: '---\n\n***\n\n___\n\n[rss-pal-anchor](#article-section-001)Text\n',
    },
    {
      name: 'preserves CRLF',
      source: '# Title\r\n\r\nParagraph\r\ncontinued\r\n',
      expected: '# [rss-pal-anchor](#article-section-001)Title\r\n\r\n[rss-pal-anchor](#article-section-002)Paragraph\r\ncontinued\r\n',
    },
    {
      name: 'backtick fence with apparent closing line that has trailing text',
      source: '```\ninside\n``` trailing\nstill inside\n```\n\nAfter\n',
      expected: '```\ninside\n``` trailing\nstill inside\n```\n\n[rss-pal-anchor](#article-section-001)After\n',
    },
    {
      name: 'tilde fence with apparent closing line that has trailing text',
      source: '~~~\ninside\n~~~ trailing\nstill inside\n~~~\n\nAfter\n',
      expected: '~~~\ninside\n~~~ trailing\nstill inside\n~~~\n\n[rss-pal-anchor](#article-section-001)After\n',
    },
  ])('ports the backend scanner fixture: $name', ({ source, expected }) => {
    expect(annotateArticleMarkdown(source)).toBe(expected)
  })

  it('anchors a meaningful blockquote after an image-only quote line', () => {
    expect(annotateArticleMarkdown('> ![](https://example.com/image.png)\n> meaningful quote\n')).toBe(
      '> ![](https://example.com/image.png)\n> [rss-pal-anchor](#article-section-001)meaningful quote\n',
    )
  })

  it('keeps an image-only middle quote in its surrounding addressable block', () => {
    expect(annotateArticleMarkdown('> text one\n> ![](https://example.com/image.png)\n> text two\n')).toBe(
      '> [rss-pal-anchor](#article-section-001)text one\n> ![](https://example.com/image.png)\n> text two\n',
    )
  })

  it('anchors an image-led paragraph when meaningful text follows', () => {
    expect(annotateArticleMarkdown('![cover](https://example.com/cover.png)\n说明文字\n')).toBe(
      '[rss-pal-anchor](#article-section-001)![cover](https://example.com/cover.png)\n说明文字\n',
    )
  })
})
