import {
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
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
  const suppressNextClickRef = useRef(false)
  const suppressTouchContextMenuRef = useRef(false)
  const [target, setTarget] = useState<ReaderContextTarget | null>(null)

  const removeTargetClasses = useCallback(() => {
    targetRef.current?.links.forEach((link) => {
      link.element.classList.remove('reader-context-target')
    })
  }, [])

  const close = useCallback(() => {
    removeTargetClasses()
    targetRef.current = null
    setTarget(null)
  }, [removeTargetClasses])

  const cancelLongPress = useCallback(() => {
    const pending = longPressRef.current
    if (pending?.timerID !== null && pending?.timerID !== undefined) {
      window.clearTimeout(pending.timerID)
    }
    longPressRef.current = null
  }, [])

  const showTarget = useCallback((next: ReaderContextTarget) => {
    if (!actionContext || actionContext.getActions(next).length === 0) {
      close()
      return
    }
    removeTargetClasses()
    next.links.forEach((link) => {
      link.element.classList.add('reader-context-target')
      actionContext.onLinkDiscovered?.({ url: link.url, title: link.title })
    })
    targetRef.current = next
    setTarget(next)
  }, [actionContext, close, removeTargetClasses])

  const openSelection = useCallback(() => {
    const root = rootRef.current
    const selection = window.getSelection()
    if (!root || !selection || !actionContext) return close()
    const links = resolveSelectionLinks(root, selection, actionContext.normalizeLink)
    if (links.length === 0) return close()
    suppressNextClickRef.current = true
    showTarget({
      kind: 'selection-links',
      links: [...links],
      anchorRect: selectionBounds(selection, links),
    })
  }, [actionContext, close, showTarget])

  const openLongPress = useCallback((anchor: HTMLAnchorElement) => {
    if (!actionContext) return
    const link = linkTargetFromAnchor(anchor, actionContext.normalizeLink)
    if (!link) return
    suppressNextClickRef.current = true
    suppressTouchContextMenuRef.current = true
    showTarget({
      kind: 'long-press-link',
      links: [link],
      anchorRect: immutableRect(anchor.getBoundingClientRect()),
    })
  }, [actionContext, showTarget])

  useEffect(() => close(), [articleKey, close])

  useEffect(() => {
    const handleScroll = () => {
      cancelLongPress()
      close()
    }
    window.addEventListener('scroll', handleScroll, true)
    return () => window.removeEventListener('scroll', handleScroll, true)
  }, [cancelLongPress, close])

  useEffect(() => () => {
    cancelLongPress()
    removeTargetClasses()
  }, [cancelLongPress, removeTargetClasses])

  const actions = useMemo(
    () => target && actionContext ? actionContext.getActions(target) : [],
    [actionContext, target],
  )

  const handlePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.pointerType !== 'touch' || !actionContext) return
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
    openSelection()
  }

  const handlePointerCancel = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!longPressRef.current || longPressRef.current.pointerID === event.pointerId) {
      cancelLongPress()
    }
  }

  const handleClickCapture = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (!suppressNextClickRef.current) return
    suppressNextClickRef.current = false
    event.preventDefault()
    event.stopPropagation()
  }

  const handleContextMenu = (event: ReactMouseEvent<HTMLDivElement>) => {
    const pointerType = (event.nativeEvent as MouseEvent & { pointerType?: string }).pointerType
    if (!suppressTouchContextMenuRef.current || pointerType === 'mouse') return
    suppressTouchContextMenuRef.current = false
    event.preventDefault()
  }

  return (
    <>
      <div
        ref={rootRef}
        className={['reader-interaction-surface', className].filter(Boolean).join(' ')}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerCancel={handlePointerCancel}
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
