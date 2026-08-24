export const ARTICLE_ANCHOR_LABEL = 'rss-pal-anchor'
export const ARTICLE_ANCHOR_RE = /^#article-section-(?:00[1-9]|0[1-9][0-9]|[1-9][0-9]{2,})$/

const ARTICLE_ANCHOR_PREFIX = 'article-section-'
const ATX_HEADING_RE = /^ {0,3}#{1,6}(?:\s|$)/
const LIST_ITEM_RE = /^\s*(?:[*+-]|[0-9]+[.)])\s+/
const BLOCKQUOTE_RE = /^ {0,3}>\s?/
const IMAGE_RE = /!\[[^\]]*\]\([^)]+\)/g
const LINK_RE = /\[([^\]]*)\]\([^)]+\)/g

type ArticleAnchorLine = {
  text: string
  ending: string
}

enum ArticleBlockKind {
  None,
  Paragraph,
  Heading,
  Blockquote,
  List,
}

export function parseArticleAnchor(href: string | undefined): string | null {
  return href && ARTICLE_ANCHOR_RE.test(href) ? href.slice(1) : null
}

function articleAnchorID(index: number): string {
  return `${ARTICLE_ANCHOR_PREFIX}${index.toString().padStart(3, '0')}`
}

function marker(index: number): string {
  return `[${ARTICLE_ANCHOR_LABEL}](#${articleAnchorID(index)})`
}

function splitLines(source: string): ArticleAnchorLine[] {
  if (!source) return []

  const lines: ArticleAnchorLine[] = []
  let start = 0
  while (start < source.length) {
    const end = source.indexOf('\n', start)
    if (end < 0) {
      lines.push({ text: source.slice(start), ending: '' })
      break
    }
    const content = source.slice(start, end)
    lines.push(content.endsWith('\r')
      ? { text: content.slice(0, -1), ending: '\r\n' }
      : { text: content, ending: '\n' })
    start = end + 1
  }
  return lines
}

function fenceStart(line: string): { char: string, length: number } | null {
  const trimmed = line.replace(/^ +/, '')
  if (trimmed.length < 3 || (trimmed[0] !== '`' && trimmed[0] !== '~')) return null
  const char = trimmed[0]
  let length = 0
  while (trimmed[length] === char) length++
  return length >= 3 ? { char, length } : null
}

function isFence(line: string, char: string, minimumLength: number): boolean {
  const trimmed = line.replace(/^ +/, '')
  if (trimmed.length < minimumLength || trimmed[0] !== char) return false
  let length = 0
  while (trimmed[length] === char) length++
  return length >= minimumLength && trimmed.slice(length).trim() === ''
}

function lineKind(line: string, active: ArticleBlockKind): { kind: ArticleBlockKind, start: boolean } {
  if (ATX_HEADING_RE.test(line)) return { kind: ArticleBlockKind.Heading, start: true }
  if (LIST_ITEM_RE.test(line)) return { kind: ArticleBlockKind.List, start: true }
  if (BLOCKQUOTE_RE.test(line)) return { kind: ArticleBlockKind.Blockquote, start: active !== ArticleBlockKind.Blockquote }
  if (active === ArticleBlockKind.List && (/^ /.test(line) || /^\t/.test(line))) {
    return { kind: ArticleBlockKind.List, start: false }
  }
  return { kind: ArticleBlockKind.Paragraph, start: active !== ArticleBlockKind.Paragraph }
}

function isThematicBreak(line: string): boolean {
  const trimmed = line.trim()
  if (trimmed.length < 3 || !['-', '*', '_'].includes(trimmed[0])) return false
  const char = trimmed[0]
  let count = 0
  for (const candidate of trimmed) {
    if (candidate === char) {
      count++
    } else if (candidate !== ' ' && candidate !== '\t') {
      return false
    }
  }
  return count >= 3
}

function isImageOnly(line: string, kind: ArticleBlockKind): boolean {
  let text = line
  if (kind === ArticleBlockKind.Heading) {
    text = text.trim().replace(/^#+/, '').trim()
  } else if (kind === ArticleBlockKind.List) {
    text = text.replace(LIST_ITEM_RE, '')
  } else if (kind === ArticleBlockKind.Blockquote) {
    text = text.replace(BLOCKQUOTE_RE, '')
  }
  return text.replace(IMAGE_RE, '').replace(LINK_RE, '$1').trim() === ''
}

function paragraphHasMeaningfulContinuation(lines: ArticleAnchorLine[], start: number): boolean {
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i]
    if (line.text.trim() === '' || isThematicBreak(line.text) || fenceStart(line.text)) return false
    if (lineKind(line.text, ArticleBlockKind.Paragraph).kind !== ArticleBlockKind.Paragraph) return false
    if (!isImageOnly(line.text, ArticleBlockKind.Paragraph)) return true
  }
  return false
}

function insertMarker(line: string, kind: ArticleBlockKind, index: number): string {
  const anchor = marker(index)
  if (kind === ArticleBlockKind.Heading) {
    return line.replace(/^( {0,3}#{1,6})(?:[ \t]+|$)/, (_match, prefix: string) => `${prefix} ${anchor}`)
  }
  if (kind === ArticleBlockKind.List) return line.replace(LIST_ITEM_RE, (prefix) => `${prefix}${anchor}`)
  if (kind === ArticleBlockKind.Blockquote) return line.replace(BLOCKQUOTE_RE, (prefix) => `${prefix}${anchor}`)
  return `${anchor}${line}`
}

function nextActiveBlock(kind: ArticleBlockKind, imageOnly: boolean, active: ArticleBlockKind): ArticleBlockKind {
  return imageOnly && kind === ArticleBlockKind.Blockquote && active !== ArticleBlockKind.Blockquote
    ? ArticleBlockKind.None
    : kind
}

// annotateArticleMarkdown mirrors backend/internal/ai/article_anchors.go's
// block scanner. It embeds the marker in the matching Markdown block so the
// rendered target has no visible text and stays inside that block's semantics.
export function annotateArticleMarkdown(source: string): string {
  const lines = splitLines(source)
  if (lines.length === 0) return source

  let nextID = 1
  let active = ArticleBlockKind.None
  let fence: { char: string, length: number } | null = null
  let output = ''

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (fence) {
      output += line.text + line.ending
      if (isFence(line.text, fence.char, fence.length)) fence = null
      continue
    }

    const openedFence = fenceStart(line.text)
    if (openedFence) {
      active = ArticleBlockKind.None
      fence = openedFence
      output += line.text + line.ending
      continue
    }

    if (line.text.trim() === '' || isThematicBreak(line.text)) {
      active = ArticleBlockKind.None
      output += line.text + line.ending
      continue
    }

    const { kind, start } = lineKind(line.text, active)
    let imageOnly = isImageOnly(line.text, kind)
    if (imageOnly && kind === ArticleBlockKind.Paragraph && active !== ArticleBlockKind.Paragraph && paragraphHasMeaningfulContinuation(lines, i)) {
      imageOnly = false
    }
    output += (start && !imageOnly ? insertMarker(line.text, kind, nextID++) : line.text) + line.ending
    active = nextActiveBlock(kind, imageOnly, active)
  }

  return output
}
