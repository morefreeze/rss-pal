const RETURN_LABEL = '跳回 AI 总结'
type ActiveRoundTrip = {
  source: HTMLAnchorElement
  cleanup: () => void
}

type ArticleAnchorRoundTripOptions = {
  onReturn?: () => void
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

// Keep text targets inline; structural/media blocks get a sibling control row.
function inlineReturnContainer(target: HTMLElement): HTMLElement | null {
  if (target.matches('p, h1, h2, h3, h4, h5, h6')) return target
  if (target.matches('li, blockquote')) {
    const last = target.lastElementChild
    if (last instanceof HTMLElement && last.matches('p, li, blockquote')) {
      return inlineReturnContainer(last)
    }
    if (target.matches('li') && !target.querySelector('p, ul, ol, table, pre, div')) return target
  }
  return null
}

export function clearArticleAnchorRoundTrip(source?: HTMLAnchorElement): void {
  if (!activeRoundTrip || (source && activeRoundTrip.source !== source)) return
  activeRoundTrip.cleanup()
}

export function startArticleAnchorRoundTrip(
  source: HTMLAnchorElement,
  target: HTMLElement,
  options: ArticleAnchorRoundTripOptions = {},
): void {
  clearArticleAnchorRoundTrip()
  if (!source.isConnected || !target.isConnected) return

  const returnLink = document.createElement('a')
  returnLink.className = 'article-anchor-return-link'
  returnLink.href = `#${source.id}`
  returnLink.textContent = '↩⌖'
  returnLink.setAttribute('aria-label', RETURN_LABEL)
  returnLink.title = RETURN_LABEL
  const inlineContainer = inlineReturnContainer(target)
  const row = inlineContainer ? null : document.createElement('div')
  if (row) {
    row.className = 'article-anchor-return-row'
    row.append(returnLink)
    target.after(row)
  } else {
    inlineContainer!.append(returnLink)
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
      options.onReturn?.()
    }
    clearArticleAnchorRoundTrip(source)
  }

  const onAuxClick = (event: MouseEvent) => {
    event.preventDefault()
  }

  returnLink.addEventListener('click', onClick)
  returnLink.addEventListener('auxclick', onAuxClick)

  const observer = new MutationObserver(() => {
    if (!source.isConnected || !target.isConnected || !returnLink.isConnected) {
      clearArticleAnchorRoundTrip(source)
    }
  })
  observer.observe(document.body, { childList: true, subtree: true })

  const cleanup = () => {
    observer.disconnect()
    returnLink.removeEventListener('click', onClick)
    returnLink.removeEventListener('auxclick', onAuxClick)
    returnLink.remove()
    row?.remove()
    if (activeRoundTrip?.cleanup === cleanup) activeRoundTrip = null
  }

  activeRoundTrip = { source, cleanup }
}
