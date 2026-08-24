export const ARTICLE_ANCHOR_RE = /^#article-section-(?:00[1-9]|0[1-9][0-9]|[1-9][0-9]{2,})$/

const ARTICLE_ANCHOR_PREFIX = 'article-section-'
const ATX_HEADING_RE = /^ {0,3}#{1,6}(?:\s|$)/
const LIST_ITEM_RE = /^\s*(?:[*+-]|[0-9]+[.)])\s+/
const BLOCKQUOTE_RE = /^ {0,3}>\s?/
const IMAGE_RE = /!\[[^\]]*\]\([^)]+\)/g
const LINK_RE = /\[([^\]]*)\]\([^)]+\)/g

export type ArticleAnchorKind = 'paragraph' | 'heading' | 'blockquote' | 'list'
export type ArticleAnchor = { id: string, kind: ArticleAnchorKind, line: number }

type ArticleAnchorLine = { text: string, ending: string }
type ArticleBlockKind = ArticleAnchorKind | 'none'

export function parseArticleAnchor(href: string | undefined): string | null {
  return href && ARTICLE_ANCHOR_RE.test(href) ? href.slice(1) : null
}

function articleAnchorID(index: number): string {
  return `${ARTICLE_ANCHOR_PREFIX}${index.toString().padStart(3, '0')}`
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
    const text = source.slice(start, end)
    lines.push(text.endsWith('\r') ? { text: text.slice(0, -1), ending: '\r\n' } : { text, ending: '\n' })
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

function isTopLevelIndentedCode(line: string): boolean {
  return /^ {4}|^\t/.test(line)
}

function lineKind(line: string, active: ArticleBlockKind): { kind: ArticleBlockKind, start: boolean } {
  if (ATX_HEADING_RE.test(line)) return { kind: 'heading', start: true }
  if (LIST_ITEM_RE.test(line)) return { kind: 'list', start: true }
  if (BLOCKQUOTE_RE.test(line)) return { kind: 'blockquote', start: active !== 'blockquote' }
  if (active === 'list' && (/^ /.test(line) || /^\t/.test(line))) return { kind: 'list', start: false }
  return { kind: 'paragraph', start: active !== 'paragraph' }
}

function isThematicBreak(line: string): boolean {
  const trimmed = line.trim()
  if (trimmed.length < 3 || !['-', '*', '_'].includes(trimmed[0])) return false
  const char = trimmed[0]
  let count = 0
  for (const candidate of trimmed) {
    if (candidate === char) count++
    else if (candidate !== ' ' && candidate !== '\t') return false
  }
  return count >= 3
}

function isImageOnly(line: string, kind: ArticleBlockKind): boolean {
  let text = line
  if (kind === 'heading') text = text.trim().replace(/^#+/, '').trim()
  else if (kind === 'list') text = text.replace(LIST_ITEM_RE, '')
  else if (kind === 'blockquote') text = text.replace(BLOCKQUOTE_RE, '')
  return text.replace(IMAGE_RE, '').replace(LINK_RE, '$1').trim() === ''
}

function paragraphHasMeaningfulContinuation(lines: ArticleAnchorLine[], start: number): boolean {
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i]
    if (line.text.trim() === '' || isThematicBreak(line.text) || fenceStart(line.text) || isTopLevelIndentedCode(line.text)) return false
    if (lineKind(line.text, 'paragraph').kind !== 'paragraph') return false
    if (!isImageOnly(line.text, 'paragraph')) return true
  }
  return false
}

function listHasContinuation(lines: ArticleAnchorLine[], blankLine: number): boolean {
  for (let i = blankLine + 1; i < lines.length; i++) {
    const text = lines[i].text
    if (text.trim() === '') continue
    return /^ /.test(text) || /^\t/.test(text)
  }
  return false
}

function nextActiveBlock(kind: ArticleBlockKind, imageOnly: boolean, active: ArticleBlockKind): ArticleBlockKind {
  return imageOnly && kind === 'blockquote' && active !== 'blockquote' ? 'none' : kind
}

// Returns out-of-band source positions. It deliberately never mutates author
// Markdown: ReactMarkdown's remark plugin applies these records to AST blocks.
export function findArticleAnchors(source: string): ArticleAnchor[] {
  const lines = splitLines(source)
  const anchors: ArticleAnchor[] = []
  let active: ArticleBlockKind = 'none'
  let fence: { char: string, length: number } | null = null

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (fence) {
      if (isFence(line.text, fence.char, fence.length)) fence = null
      continue
    }
    const openedFence = fenceStart(line.text)
    if (openedFence) {
      active = 'none'
      fence = openedFence
      continue
    }
    if (line.text.trim() === '' || isThematicBreak(line.text)) {
      active = active === 'list' && listHasContinuation(lines, i) ? 'list' : 'none'
      continue
    }
    if (isTopLevelIndentedCode(line.text)) {
      active = 'none'
      continue
    }

    const { kind, start } = lineKind(line.text, active)
    let imageOnly = isImageOnly(line.text, kind)
    if (imageOnly && kind === 'paragraph' && active !== 'paragraph' && paragraphHasMeaningfulContinuation(lines, i)) imageOnly = false
    if (start && !imageOnly && kind !== 'none') anchors.push({ id: articleAnchorID(anchors.length + 1), kind, line: i + 1 })
    active = nextActiveBlock(kind, imageOnly, active)
  }
  return anchors
}

// Kept as the cleanup-pipeline API: body markdown must remain byte-for-byte
// author content; IDs are assigned out-of-band by createArticleAnchorRemarkPlugin.
export function annotateArticleMarkdown(source: string): string {
  return source
}

type MarkdownNode = {
  type?: string
  position?: { start?: { line?: number }, end?: { line?: number } }
  children?: MarkdownNode[]
  data?: { hProperties?: Record<string, unknown> }
}

export function createArticleAnchorRemarkPlugin(anchors: ArticleAnchor[]) {
  return () => (tree: MarkdownNode) => {
    const candidates: MarkdownNode[] = []
    const collect = (node: MarkdownNode) => {
      if (node.type === 'heading' || node.type === 'paragraph' || node.type === 'blockquote' || node.type === 'listItem' || node.type === 'table') candidates.push(node)
      node.children?.forEach(collect)
    }
    collect(tree)

    const used = new Set<MarkdownNode>()
    for (const anchor of anchors) {
      const node = candidates.find((candidate) => {
        if (used.has(candidate)) return false
        const start = candidate.position?.start?.line
        const end = candidate.position?.end?.line ?? start
        if (!start || !end || anchor.line < start || anchor.line > end) return false
        if (anchor.kind === 'blockquote') return candidate.type === 'blockquote'
        if (anchor.kind === 'list') return candidate.type === 'listItem' && start === anchor.line
        return start === anchor.line && (candidate.type === 'heading' || candidate.type === 'table' || candidate.type === 'paragraph')
      })
      if (!node) continue
      used.add(node)
      node.data ??= {}
      node.data.hProperties = { ...node.data.hProperties, id: anchor.id, className: 'article-section-anchor' }
    }
  }
}
