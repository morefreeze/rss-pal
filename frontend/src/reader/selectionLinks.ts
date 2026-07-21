import type { ReaderLinkTarget } from './types'

type NormalizeLink = (href: string) => string | null

function isInside(root: HTMLElement, node: Node): boolean {
  return node === root || root.contains(node)
}

function textOffset(root: HTMLElement, container: Node, offset: number): number {
  const prefix = document.createRange()
  prefix.selectNodeContents(root)
  prefix.setEnd(container, offset)
  return prefix.toString().length
}

function anchorTitle(anchor: HTMLAnchorElement, url: string): string {
  const text = (anchor.textContent ?? '').trim().replace(/\s+/g, ' ')
  if (text) return text
  try {
    return new URL(url).hostname || url
  } catch {
    return url
  }
}

export function linkTargetFromAnchor(
  anchor: HTMLAnchorElement,
  normalizeLink: NormalizeLink,
): ReaderLinkTarget | null {
  const href = anchor.getAttribute('href')
  if (!href) return null
  const url = normalizeLink(href)
  if (!url) return null
  return { url, title: anchorTitle(anchor, url), element: anchor }
}

export function resolveSelectionLinks(
  root: HTMLElement,
  selection: Selection,
  normalizeLink: NormalizeLink,
): ReaderLinkTarget[] {
  if (selection.rangeCount === 0 || selection.isCollapsed) return []
  const selectedRange = selection.getRangeAt(0)
  if (
    !isInside(root, selectedRange.startContainer)
    || !isInside(root, selectedRange.endContainer)
  ) return []

  const selectionStart = textOffset(root, selectedRange.startContainer, selectedRange.startOffset)
  const selectionEnd = textOffset(root, selectedRange.endContainer, selectedRange.endOffset)
  if (selectionEnd <= selectionStart) return []

  const seen = new Set<string>()
  const targets: ReaderLinkTarget[] = []
  for (const anchor of root.querySelectorAll<HTMLAnchorElement>('a[href]')) {
    if (anchor.closest('pre, code')) continue
    const anchorRange = document.createRange()
    anchorRange.selectNodeContents(anchor)
    const anchorStart = textOffset(root, anchorRange.startContainer, anchorRange.startOffset)
    const anchorEnd = textOffset(root, anchorRange.endContainer, anchorRange.endOffset)
    if (Math.max(selectionStart, anchorStart) >= Math.min(selectionEnd, anchorEnd)) continue

    const target = linkTargetFromAnchor(anchor, normalizeLink)
    if (!target || seen.has(target.url)) continue
    seen.add(target.url)
    targets.push(target)
  }
  return targets
}

export const selectionLinkTargets = resolveSelectionLinks
