const RETURN_LABEL = '跳回 AI 总结'
const RETURN_LINK_GAP = 4
const RETURN_LINK_VIEWPORT_MARGIN = 8
const RETURN_LINK_FALLBACK_WIDTH = 36

type ActiveRoundTrip = {
  source: HTMLAnchorElement
  cleanup: () => void
}

let activeRoundTrip: ActiveRoundTrip | null = null

function prefersReducedMotion(): boolean {
  return window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
}

function isPlainPrimaryClick(event: MouseEvent): boolean {
  return event.button === 0
    && !event.metaKey
    && !event.ctrlKey
    && !event.shiftKey
    && !event.altKey
}

function finalContentRect(target: HTMLElement): DOMRect | null {
  if (!target.hasChildNodes()) return null
  const range = document.createRange()
  range.selectNodeContents(target)
  if (typeof range.getClientRects !== 'function') return null
  const rects = Array.from(range.getClientRects()).filter(rect => rect.width > 0 || rect.height > 0)
  return rects[rects.length - 1] ?? null
}

export function clearArticleAnchorRoundTrip(source?: HTMLAnchorElement): void {
  if (!activeRoundTrip || (source && activeRoundTrip.source !== source)) return
  activeRoundTrip.cleanup()
}

export function startArticleAnchorRoundTrip(
  source: HTMLAnchorElement,
  target: HTMLElement,
): void {
  clearArticleAnchorRoundTrip()

  const returnLink = document.createElement('a')
  returnLink.className = 'article-anchor-return-link'
  returnLink.href = `#${source.id}`
  returnLink.textContent = '↩⌖'
  returnLink.setAttribute('aria-label', RETURN_LABEL)
  returnLink.title = RETURN_LABEL
  document.body.append(returnLink)

  const updatePosition = () => {
    if (!source.isConnected || !target.isConnected) {
      clearArticleAnchorRoundTrip(source)
      return
    }
    const targetRect = target.getBoundingClientRect()
    const outsideViewport = targetRect.bottom < 0
      || targetRect.top > window.innerHeight
      || targetRect.right < 0
      || targetRect.left > window.innerWidth
    returnLink.hidden = outsideViewport

    const contentRect = finalContentRect(target)
    const returnWidth = returnLink.getBoundingClientRect().width || RETURN_LINK_FALLBACK_WIDTH
    const maxLeft = Math.max(
      RETURN_LINK_VIEWPORT_MARGIN,
      window.innerWidth - returnWidth - RETURN_LINK_VIEWPORT_MARGIN,
    )

    if (contentRect) {
      const afterTextLeft = contentRect.right + RETURN_LINK_GAP
      const fitsAfterText = afterTextLeft + returnWidth <= window.innerWidth - RETURN_LINK_VIEWPORT_MARGIN
      returnLink.style.left = `${fitsAfterText
        ? afterTextLeft
        : Math.min(Math.max(contentRect.right - returnWidth, RETURN_LINK_VIEWPORT_MARGIN), maxLeft)}px`
      returnLink.style.top = `${fitsAfterText ? contentRect.top : contentRect.bottom + 2}px`
      return
    }

    returnLink.style.left = `${Math.min(
      Math.max(targetRect.right - returnWidth, RETURN_LINK_VIEWPORT_MARGIN),
      maxLeft,
    )}px`
    returnLink.style.top = `${Math.max(targetRect.top, RETURN_LINK_VIEWPORT_MARGIN)}px`
  }

  const onClick = (event: MouseEvent) => {
    event.preventDefault()
    if (!isPlainPrimaryClick(event)) return

    if (source.isConnected) {
      source.scrollIntoView({
        behavior: prefersReducedMotion() ? 'auto' : 'smooth',
        block: 'center',
      })
      if (event.detail === 0) source.focus({ preventScroll: true })
    }
    clearArticleAnchorRoundTrip(source)
  }

  const onAuxClick = (event: MouseEvent) => {
    event.preventDefault()
  }

  const scrollOptions: AddEventListenerOptions = { passive: true }
  window.addEventListener('scroll', updatePosition, scrollOptions)
  window.addEventListener('resize', updatePosition)
  returnLink.addEventListener('click', onClick)
  returnLink.addEventListener('auxclick', onAuxClick)

  const resizeObserver = typeof ResizeObserver === 'undefined'
    ? null
    : new ResizeObserver(updatePosition)
  resizeObserver?.observe(target)

  const cleanup = () => {
    window.removeEventListener('scroll', updatePosition, scrollOptions)
    window.removeEventListener('resize', updatePosition)
    resizeObserver?.disconnect()
    returnLink.removeEventListener('click', onClick)
    returnLink.removeEventListener('auxclick', onAuxClick)
    returnLink.remove()
    if (activeRoundTrip?.cleanup === cleanup) activeRoundTrip = null
  }

  activeRoundTrip = { source, cleanup }
  updatePosition()
}
