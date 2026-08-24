import { useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import type { Components, ExtraProps } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { parseArticleAnchor } from '../util/articleAnchors'

type Props = {
  source: string
}

const REMARK_PLUGINS = [remarkGfm]
const ARTICLE_HIGHLIGHT_TIMEOUT_MS = 7_100
const ARTICLE_LINK_LABEL = '跳转原文'

const activeHighlights = new WeakMap<HTMLElement, () => void>()

function restartArticleHighlight(target: HTMLElement) {
  activeHighlights.get(target)?.()
  target.classList.remove('article-anchor-highlight')
  // Restart the CSS animation when a reader follows the same summary link.
  void target.offsetWidth
  target.classList.add('article-anchor-highlight')

  let timeout: ReturnType<typeof window.setTimeout>
  const onAnimationEnd = (event: AnimationEvent) => {
    if (event.target === target) cleanup()
  }
  const cleanup = () => {
    if (activeHighlights.get(target) !== cleanup) return
    window.clearTimeout(timeout)
    target.removeEventListener('animationend', onAnimationEnd)
    target.classList.remove('article-anchor-highlight')
    activeHighlights.delete(target)
  }

  activeHighlights.set(target, cleanup)
  target.addEventListener('animationend', onAnimationEnd)
  timeout = window.setTimeout(cleanup, ARTICLE_HIGHLIGHT_TIMEOUT_MS)
}

function isNativelyFocusable(target: HTMLElement): boolean {
  return target.matches([
    'a[href]',
    'area[href]',
    'button:not([disabled])',
    'input:not([disabled])',
    'select:not([disabled])',
    'textarea:not([disabled])',
    'iframe',
    '[contenteditable="true"]',
    '[tabindex]',
  ].join(','))
}

type SummaryLinkProps = React.AnchorHTMLAttributes<HTMLAnchorElement> & ExtraProps

function SummaryLink({ href, children, node: _node, onClick, onAuxClick, ...rest }: SummaryLinkProps) {
  const targetID = parseArticleAnchor(href)
  const handleClick = (event: React.MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event)
    if (event.defaultPrevented) return

    if (!targetID) return
    event.preventDefault()
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return

    const target = document.getElementById(targetID)
    if (!target) return

    const reducedMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
    target.scrollIntoView({ behavior: reducedMotion ? 'auto' : 'smooth', block: 'center' })
    restartArticleHighlight(target)

    if (event.detail !== 0) return
    const needsTemporaryTabIndex = !isNativelyFocusable(target)
    if (needsTemporaryTabIndex) target.setAttribute('tabindex', '-1')
    target.focus({ preventScroll: true })
    if (needsTemporaryTabIndex) target.removeAttribute('tabindex')
  }

  const handleAuxClick = (event: React.MouseEvent<HTMLAnchorElement>) => {
    onAuxClick?.(event)
    if (!event.defaultPrevented && targetID) event.preventDefault()
  }

  const className = [rest.className, targetID ? 'summary-article-link' : ''].filter(Boolean).join(' ') || undefined

  return (
    <a href={href} onClick={handleClick} onAuxClick={handleAuxClick} {...rest} className={className}>
      {targetID ? ARTICLE_LINK_LABEL : children}
      {targetID && <span className="summary-article-link-icon" aria-hidden="true">⌖</span>}
    </a>
  )
}

const COMPONENTS: Components = { a: SummaryLink }

export function normalizeSummaryMarkdown(source: string): string {
  return source.replace(/(^|\n)([ \t]*)[•▸]\s+/g, '$1$2- ')
}

export default function SummaryMarkdown({ source }: Props) {
  const normalized = useMemo(() => normalizeSummaryMarkdown(source), [source])
  return (
    <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={COMPONENTS}>
      {normalized}
    </ReactMarkdown>
  )
}
