import { describe, expect, it } from 'vitest'

import { ARTICLE_ANCHOR_RE, annotateArticleMarkdown, findArticleAnchors, parseArticleAnchor } from './articleAnchors'

describe('article anchors', () => {
  it('recognizes only canonical internal hrefs and returns their bare DOM IDs', () => {
    expect(ARTICLE_ANCHOR_RE.test('#article-section-001')).toBe(true)
    expect(ARTICLE_ANCHOR_RE.test('#article-section-1000')).toBe(true)
    expect(parseArticleAnchor('#article-section-001')).toBe('article-section-001')
    for (const href of ['#article-section-000', '#article-section-01', '#article-section-0001', '#article-section-001x']) {
      expect(parseArticleAnchor(href)).toBeNull()
    }
  })

  it.each([
    ['heading multiline paragraph inline link list and fenced code', '# Title\n\nA paragraph\nthat continues with [a link](https://example.com).\n\n- first item\n  continuation\n- ![only image](https://example.com/image.png)\n\n```go\nfmt.Println("not an article block")\n```\n\n## End\n', ['heading:1:article-section-001', 'paragraph:3:article-section-002', 'list:6:article-section-003', 'heading:14:article-section-004']],
    ['blank content', '\n  \n\t\n', []],
    ['image only block', '![cover](https://example.com/cover.png)\n\n![second](https://example.com/second.png)\n', []],
    ['link only paragraph is addressable', '[Read the report](https://example.com/report)\n', ['paragraph:1:article-section-001']],
    ['ordered and nested lists are deterministic', '1. first\n   1. nested one\n   2. nested two\n2. second\n   - nested bullet\n', ['list:1:article-section-001', 'list:2:article-section-002', 'list:3:article-section-003', 'list:4:article-section-004', 'list:5:article-section-005']],
    ['blockquote', '> quoted text\n> continued quote\n\noutside\n', ['blockquote:1:article-section-001', 'paragraph:4:article-section-002']],
    ['tilde fence', '~~~markdown\n# not a heading\n~~~\n\nAfter fence\n', ['paragraph:5:article-section-001']],
    ['thematic separators', '---\n\n***\n\n___\n\nText\n', ['paragraph:7:article-section-001']],
    ['preserves CRLF', '# Title\r\n\r\nParagraph\r\ncontinued\r\n', ['heading:1:article-section-001', 'paragraph:3:article-section-002']],
    ['backtick fence with apparent closing line that has trailing text', '```\ninside\n``` trailing\nstill inside\n```\n\nAfter\n', ['paragraph:7:article-section-001']],
    ['tilde fence with apparent closing line that has trailing text', '~~~\ninside\n~~~ trailing\nstill inside\n~~~\n\nAfter\n', ['paragraph:7:article-section-001']],
  ])('ports the backend scanner fixture: %s', (_name, source, expected) => {
    expect(annotateArticleMarkdown(source)).toBe(source)
    expect(findArticleAnchors(source).map(({ kind, line, id }) => `${kind}:${line}:${id}`)).toEqual(expected)
  })

  it('skips top-level indented code and blank-separated list continuation paragraphs', () => {
    const source = '    top-level code\n\tmore top-level code\n\n- item\n\n  continuation paragraph\n\noutside\n'
    expect(findArticleAnchors(source).map(({ kind, line, id }) => `${kind}:${line}:${id}`)).toEqual([
      'list:4:article-section-001',
      'paragraph:8:article-section-002',
    ])
  })

  it('handles image-first paragraphs and blockquotes without in-band sentinels', () => {
    expect(findArticleAnchors('![cover](https://example.com/cover.png)\n说明文字\n').map(({ id }) => id)).toEqual(['article-section-001'])
    expect(findArticleAnchors('> ![](https://example.com/image.png)\n> meaningful quote\n').map(({ line, id }) => `${line}:${id}`)).toEqual(['2:article-section-001'])
  })

  it('normalizes blank lines inside image alt text before assigning IDs', () => {
    const source = 'Intro\n\n![multi-line alt\n\ntext](https://example.com/a.png)\n\nAfter'
    expect(findArticleAnchors(source).map(({ kind, line, id }) => `${kind}:${line}:${id}`)).toEqual([
      'paragraph:1:article-section-001',
      'paragraph:5:article-section-002',
    ])
  })

  it('recognizes four-space nested lists without treating top-level indented code as a list', () => {
    const nested = '- parent\n    - nested\n- sibling'
    expect(findArticleAnchors(nested).map(({ kind, line, id }) => `${kind}:${line}:${id}`)).toEqual([
      'list:1:article-section-001',
      'list:2:article-section-002',
      'list:3:article-section-003',
    ])

    const topLevelCode = '    - top-level code\n\nText'
    expect(findArticleAnchors(topLevelCode).map(({ kind, line, id }) => `${kind}:${line}:${id}`)).toEqual([
      'paragraph:3:article-section-001',
    ])
  })
})
