import {
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react'
import { ReaderActionContext } from './ReaderActionContext'
import { ReaderContextMenu } from './ReaderContextMenu'
import { linkTargetFromAnchor, resolveSelectionLinks } from './selectionLinks'
import type { ReaderContextTarget, ReaderLinkTarget } from './types'

const LONG_PRESS_MS = 500
const LONG_PRESS_MOVE_TOLERANCE = 8
const GENERATED_EVENT_TTL_MS = 800

type ReaderInteractionSurfaceProps = {
  articleKey: string | number
  children: ReactNode
  className?: string
}

type PendingLongPress = {
  pointerID: number
  startX: number
  startY: number
  anchor: HTMLAnchorElement
  timerID: number | null
}

type GeneratedEventSuppression = {
  anchor: HTMLAnchorElement
  timerID: number
}

function immutableRect(rect: DOMRect): DOMRect {
  return new DOMRect(rect.x, rect.y, rect.width, rect.height)
}

function linkBounds(links: ReaderLinkTarget[]): DOMRect {
  const rects = links.map((link) => link.element.getBoundingClientRect())
  const left = Math.min(...rects.map((rect) => rect.left))
  const top = Math.min(...rects.map((rect) => rect.top))
  const right = Math.max(...rects.map((rect) => rect.right))
  const bottom = Math.max(...rects.map((rect) => rect.bottom))
  return new DOMRect(left, top, Math.max(0, right - left), Math.max(0, bottom - top))
}

function selectionBounds(selection: Selection, links: ReaderLinkTarget[]): DOMRect {
  const range = selection.rangeCount > 0 ? selection.getRangeAt(0) : null
  const getRangeRect = range && 'getBoundingClientRect' in range
    ? (range as Range & { getBoundingClientRect(): DOMRect }).getBoundingClientRect
    : null
  if (range && getRangeRect) return immutableRect(getRangeRect.call(range))
  return linkBounds(links)
}

export function ReaderInteractionSurface({
  articleKey,
  children,
  className,
}: ReaderInteractionSurfaceProps) {
  const actionContext = useContext(ReaderActionContext)
  const rootRef = useRef<HTMLDivElement>(null)
  const targetRef = useRef<ReaderContextTarget | null>(null)
  const longPressRef = useRef<PendingLongPress | null>(null)
  const pointerSelectingRef = useRef(false)
  const returnFocusRef = useRef<HTMLElement | null>(null)
  const clickSuppressionRef = useRef<GeneratedEventSuppression | null>(null)
  const contextMenuSuppressionRef = useRef<GeneratedEventSuppression | null>(null)
  const [target, setTarget] = useState<ReaderContextTarget | null>(null)

  const clearClickSuppression = useCallback(() => {
    const suppression = clickSuppressionRef.current
    if (suppression) window.clearTimeout(suppression.timerID)
    clickSuppressionRef.current = null
  }, [])

  const clearContextMenuSuppression = useCallback(() => {
    const suppression = contextMenuSuppressionRef.current
    if (suppression) window.clearTimeout(suppression.timerID)
    contextMenuSuppressionRef.current = null
  }, [])

  const armClickSuppression = useCallback((anchor: HTMLAnchorElement) => {
    clearClickSuppression()
    const suppression: GeneratedEventSuppression = { anchor, timerID: 0 }
    suppression.timerID = window.setTimeout(() => {
      if (clickSuppressionRef.current === suppression) clickSuppressionRef.current = null
    }, GENERATED_EVENT_TTL_MS)
    clickSuppressionRef.current = suppression
  }, [clearClickSuppression])

  const armContextMenuSuppression = useCallback((anchor: HTMLAnchorElement) => {
    clearContextMenuSuppression()
    const suppression: GeneratedEventSuppression = { anchor, timerID: 0 }
    suppression.timerID = window.setTimeout(() => {
      if (contextMenuSuppressionRef.current === suppression) contextMenuSuppressionRef.current = null
    }, GENERATED_EVENT_TTL_MS)
    contextMenuSuppressionRef.current = suppression
  }, [clearContextMenuSuppression])

  const removeTargetClasses = useCallback(() => {
    targetRef.current?.links.forEach((link) => {
      link.element.classList.remove('reader-context-target')
    })
  }, [])

  const close = useCallback(() => {
    clearClickSuppression()
    clearContextMenuSuppression()
    removeTargetClasses()
    targetRef.current = null
    setTarget(null)
    const returnFocus = returnFocusRef.current
    returnFocusRef.current = null
    if (returnFocus?.isConnected) returnFocus.focus({ preventScroll: true })
  }, [clearClickSuppression, clearContextMenuSuppression, removeTargetClasses])

  const cancelLongPress = useCallback(() => {
    const pending = longPressRef.current
    if (pending?.timerID !== null && pending?.timerID !== undefined) {
      window.clearTimeout(pending.timerID)
    }
    longPressRef.current = null
  }, [])

  const showTarget = useCallback((next: ReaderContextTarget): boolean => {
    if (!actionContext || actionContext.getActions(next).length === 0) {
      close()
      return false
    }
    removeTargetClasses()
    const root = rootRef.current
    const active = document.activeElement
    returnFocusRef.current = active instanceof HTMLElement && root?.contains(active)
      ? active
      : next.links[0]?.element ?? root
    next.links.forEach((link) => {
      link.element.classList.add('reader-context-target')
      actionContext.onLinkDiscovered?.({ url: link.url, title: link.title })
    })
    targetRef.current = next
    setTarget(next)
    return true
  }, [actionContext, close, removeTargetClasses])

  const openSelection = useCallback((clickAnchor?: HTMLAnchorElement | null) => {
    const root = rootRef.current
    const selection = window.getSelection()
    if (!root || !selection || !actionContext) return close()
    const links = resolveSelectionLinks(root, selection, actionContext.normalizeLink)
    if (links.length === 0) return close()
    const shown = showTarget({
      kind: 'selection-links',
      links: [...links],
      anchorRect: selectionBounds(selection, links),
    })
    if (shown && clickAnchor && links.some((link) => link.element === clickAnchor)) {
      armClickSuppression(clickAnchor)
    }
  }, [actionContext, armClickSuppression, close, showTarget])

  const openLongPress = useCallback((anchor: HTMLAnchorElement) => {
    if (!actionContext) return
    const link = linkTargetFromAnchor(anchor, actionContext.normalizeLink)
    if (!link) return
    const shown = showTarget({
      kind: 'long-press-link',
      links: [link],
      anchorRect: immutableRect(anchor.getBoundingClientRect()),
    })
    if (shown) {
      armClickSuppression(anchor)
      armContextMenuSuppression(anchor)
    }
  }, [actionContext, armClickSuppression, armContextMenuSuppression, showTarget])

  useEffect(() => close(), [articleKey, close])

  useEffect(() => {
    const handleScroll = () => {
      cancelLongPress()
      close()
    }
    window.addEventListener('scroll', handleScroll, true)
    return () => window.removeEventListener('scroll', handleScroll, true)
  }, [cancelLongPress, close])

  useEffect(() => {
    const handleSelectionChange = () => {
      if (pointerSelectingRef.current) return
      const selection = window.getSelection()
      if (!selection || selection.isCollapsed) {
        const menuHasFocus = document.activeElement instanceof Element
          && document.activeElement.closest('.reader-context-menu') !== null
        if (targetRef.current && !menuHasFocus) close()
        return
      }
      openSelection()
    }
    document.addEventListener('selectionchange', handleSelectionChange)
    return () => document.removeEventListener('selectionchange', handleSelectionChange)
  }, [close, openSelection])

  useEffect(() => {
    const finishPointerSelection = (event: PointerEvent) => {
      pointerSelectingRef.current = false
      const pending = longPressRef.current
      if (pending && pending.pointerID === event.pointerId) cancelLongPress()
    }
    window.addEventListener('pointerup', finishPointerSelection, true)
    window.addEventListener('pointercancel', finishPointerSelection, true)
    return () => {
      window.removeEventListener('pointerup', finishPointerSelection, true)
      window.removeEventListener('pointercancel', finishPointerSelection, true)
    }
  }, [cancelLongPress])

  useEffect(() => () => {
    cancelLongPress()
    clearClickSuppression()
    clearContextMenuSuppression()
    removeTargetClasses()
  }, [cancelLongPress, clearClickSuppression, clearContextMenuSuppression, removeTargetClasses])

  const actions = useMemo(
    () => target && actionContext ? actionContext.getActions(target) : [],
    [actionContext, target],
  )

  const handlePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.pointerType !== 'touch') {
      pointerSelectingRef.current = event.button === 0
      return
    }
    if (!actionContext) return
    const source = event.target instanceof Element ? event.target : null
    const anchor = source?.closest<HTMLAnchorElement>('a[href]') ?? null
    if (!anchor || !rootRef.current?.contains(anchor)) return
    if (!linkTargetFromAnchor(anchor, actionContext.normalizeLink)) return

    cancelLongPress()
    const pending: PendingLongPress = {
      pointerID: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      anchor,
      timerID: null,
    }
    pending.timerID = window.setTimeout(() => {
      if (longPressRef.current !== pending) return
      pending.timerID = null
      openLongPress(anchor)
    }, LONG_PRESS_MS)
    longPressRef.current = pending
  }

  const handlePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const pending = longPressRef.current
    if (!pending || pending.pointerID !== event.pointerId) return
    if (
      Math.abs(event.clientX - pending.startX) > LONG_PRESS_MOVE_TOLERANCE
      || Math.abs(event.clientY - pending.startY) > LONG_PRESS_MOVE_TOLERANCE
    ) cancelLongPress()
  }

  const handlePointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.pointerType === 'touch') {
      cancelLongPress()
      return
    }
    pointerSelectingRef.current = false
    if (event.button !== 0) return
    const source = event.target instanceof Element ? event.target : null
    const clickAnchor = source?.closest<HTMLAnchorElement>('a[href]') ?? null
    openSelection(clickAnchor)
  }

  const handlePointerCancel = (event: ReactPointerEvent<HTMLDivElement>) => {
    pointerSelectingRef.current = false
    if (!longPressRef.current || longPressRef.current.pointerID === event.pointerId) {
      cancelLongPress()
    }
  }

  const handleKeyUp = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (
      event.key === 'Shift'
      || event.key.startsWith('Arrow')
      || event.key === 'Home'
      || event.key === 'End'
      || event.key === 'PageUp'
      || event.key === 'PageDown'
    ) openSelection()
  }

  const handleClickCapture = (event: ReactMouseEvent<HTMLDivElement>) => {
    const suppression = clickSuppressionRef.current
    const eventTarget = event.target instanceof Node ? event.target : null
    if (!suppression || !eventTarget || !suppression.anchor.contains(eventTarget)) return
    clearClickSuppression()
    event.preventDefault()
    event.stopPropagation()
  }

  const handleContextMenu = (event: ReactMouseEvent<HTMLDivElement>) => {
    const pointerType = (event.nativeEvent as MouseEvent & { pointerType?: string }).pointerType
    const suppression = contextMenuSuppressionRef.current
    const eventTarget = event.target instanceof Node ? event.target : null
    if (
      !suppression
      || pointerType === 'mouse'
      || !eventTarget
      || !suppression.anchor.contains(eventTarget)
    ) return
    clearContextMenuSuppression()
    event.preventDefault()
  }

  return (
    <>
      <div
        ref={rootRef}
        tabIndex={-1}
        className={['reader-interaction-surface', className].filter(Boolean).join(' ')}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerCancel={handlePointerCancel}
        onKeyUp={handleKeyUp}
        onClickCapture={handleClickCapture}
        onContextMenu={handleContextMenu}
      >
        {children}
      </div>
      <ReaderContextMenu
        open={target !== null}
        anchorRect={target?.anchorRect ?? null}
        actions={actions}
        onClose={close}
      />
    </>
  )
}
