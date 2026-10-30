import { parsePlaceholder } from '../components/parseVideoPlaceholder'
import type { Root, Element, Text, RootContent } from 'hast'
import { findSearchMatches } from './searchMatches'

// Process an inline run as one string, then map its ranges back to individual
// text nodes. One hit split by emphasis/link/code nodes keeps one logical ID.
export function createSearchHighlightPlugin(query: string, prefix: string) {
  return function searchHighlight() {
    return (tree: Root) => {
      if (!query.trim()) return
      let sequence = 0
      const excluded = new Set(['script', 'style', 'annotation', 'math', 'button', 'svg'])
      const blocks = new Set(['p', 'li', 'pre', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'td', 'th', 'blockquote', 'div'])
      type Parent = Root | Element
      type Part = { node: Text; parent: Parent; offset: number }
      let parts: Part[] = []
      let text = ''
      const flush = () => {
        const ranges = findSearchMatches(text, query).map(range => ({ ...range, id: `${prefix}-${sequence++}` }))
        for (const { node, parent, offset } of parts) {
          const replacements: RootContent[] = []
          let cursor = 0
          for (const range of ranges) {
            const start = Math.max(0, range.start - offset)
            const end = Math.min(node.value.length, range.end - offset)
            if (start >= end) continue
            if (start > cursor) replacements.push({ type: 'text', value: node.value.slice(cursor, start) })
            replacements.push({ type: 'element', tagName: 'mark', properties: { className: ['search-match'], 'data-search-match': range.id }, children: [{ type: 'text', value: node.value.slice(start, end) }] })
            cursor = end
          }
          if (!replacements.length) continue
          if (cursor < node.value.length) replacements.push({ type: 'text', value: node.value.slice(cursor) })
          const index = parent.children.indexOf(node)
          parent.children.splice(index, 1, ...replacements as Element['children'])
        }
        parts = []; text = ''
      }
      const walk = (parent: Parent) => {
        for (const child of [...parent.children]) {
          if (child.type === 'text') {
            parts.push({ node: child, parent, offset: text.length }); text += child.value
          } else if (child.type === 'element') {
            if (child.tagName === 'p' && child.children.length === 1 && child.children[0].type === 'text' && parsePlaceholder(child.children[0].value)) { flush(); continue }
            const classes = String(child.properties.className || '')
            if (excluded.has(child.tagName) || classes.includes('katex-mathml') || classes.includes('summary-article-link')) { flush(); continue }
            if (child.tagName === 'br' || blocks.has(child.tagName)) { flush(); walk(child); flush() }
            else walk(child)
          }
        }
      }
      walk(tree); flush()
    }
  }
}
