const RETURN_LABEL = '跳回 AI 总结'

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
    const rect = target.getBoundingClientRect()
    const outsideViewport = rect.bottom < 0
      || rect.top > window.innerHeight
      || rect.right < 0
      || rect.left > window.innerWidth
    returnLink.hidden = outsideViewport
    returnLink.style.left = `${Math.min(Math.max(rect.right, 8), window.innerWidth - 8)}px`
    returnLink.style.top = `${Math.max(rect.top, 8)}px`
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

