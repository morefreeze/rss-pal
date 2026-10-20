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
import { useBreakpoint } from '../hooks/useBreakpoint'
import { ReaderActionContext } from './ReaderActionContext'
import { ReaderContextMenu } from './ReaderContextMenu'
import { resolveSelectionLinks } from './selectionLinks'
import type { ReaderContextTarget, ReaderLinkTarget } from './types'

const GENERATED_EVENT_TTL_MS = 800

type ReaderInteractionSurfaceProps = {
  articleKey: string | number
  children: ReactNode
  className?: string
  lang?: string
}

type GeneratedEventSuppression = {
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
  lang,
}: ReaderInteractionSurfaceProps) {
  const narrow = useBreakpoint() === 'phone'
  const [coarsePointer, setCoarsePointer] = useState(() => window.matchMedia('(pointer: coarse)').matches)
  const touchUI = narrow || coarsePointer
  useEffect(() => {
    const query = window.matchMedia('(pointer: coarse)')
    const update = () => setCoarsePointer(query.matches)
    query.addEventListener('change', update)
    return () => query.removeEventListener('change', update)
  }, [])
  const actionContext = useContext(ReaderActionContext)
  const rootRef = useRef<HTMLDivElement>(null)
  const targetRef = useRef<ReaderContextTarget | null>(null)
  const pointerSelectingRef = useRef(false)
  const returnFocusRef = useRef<HTMLElement | null>(null)
  const clickSuppressionRef = useRef<GeneratedEventSuppression | null>(null)
  const [target, setTarget] = useState<ReaderContextTarget | null>(null)

  const clearClickSuppression = useCallback(() => {
    const suppression = clickSuppressionRef.current
    if (suppression?.timerID !== null && suppression?.timerID !== undefined) {
      window.clearTimeout(suppression.timerID)
    }
    clickSuppressionRef.current = null
  }, [])

  const armClickSuppression = useCallback((anchor: HTMLAnchorElement) => {
    clearClickSuppression()
    const suppression: GeneratedEventSuppression = { anchor, timerID: null }
    suppression.timerID = window.setTimeout(() => {
      if (clickSuppressionRef.current === suppression) clickSuppressionRef.current = null
    }, GENERATED_EVENT_TTL_MS)
    clickSuppressionRef.current = suppression
  }, [clearClickSuppression])

  const removeTargetClasses = useCallback(() => {
    targetRef.current?.links.forEach((link) => {
      link.element.classList.remove('reader-context-target')
    })
  }, [])

  const close = useCallback(() => {
    clearClickSuppression()
    removeTargetClasses()
    targetRef.current = null
    setTarget(null)
    const returnFocus = returnFocusRef.current
    returnFocusRef.current = null
    if (!touchUI && returnFocus?.isConnected) returnFocus.focus({ preventScroll: true })
  }, [clearClickSuppression, touchUI, removeTargetClasses])

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

  useEffect(() => close(), [articleKey, close])

  useEffect(() => {
    const handleScroll = () => {
      if (!touchUI) close()
    }
    window.addEventListener('scroll', handleScroll, true)
    return () => window.removeEventListener('scroll', handleScroll, true)
  }, [close, touchUI])

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
    const finish = () => { pointerSelectingRef.current = false }
    window.addEventListener('pointerup', finish, true)
    window.addEventListener('pointercancel', finish, true)
    return () => {
      window.removeEventListener('pointerup', finish, true)
      window.removeEventListener('pointercancel', finish, true)
    }
  }, [])

  useEffect(() => () => {
    clearClickSuppression()
    removeTargetClasses()
  }, [clearClickSuppression, removeTargetClasses])

  const actions = useMemo(
    () => target && actionContext ? actionContext.getActions(target) : [],
    [actionContext, target],
  )

  // Native touch long press owns link preview/share and text selection.
  const handlePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.pointerType !== 'touch') pointerSelectingRef.current = event.button === 0
  }

  const handlePointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.pointerType === 'touch') return
    pointerSelectingRef.current = false
    if (event.button !== 0) return
    const source = event.target instanceof Element ? event.target : null
    const clickAnchor = source?.closest<HTMLAnchorElement>('a[href]') ?? null
    openSelection(clickAnchor)
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

  return (
    <>
      <div
        ref={rootRef}
        tabIndex={-1}
        className={['reader-interaction-surface', className].filter(Boolean).join(' ')}
        lang={lang}
        onPointerDown={handlePointerDown}
        onPointerUp={handlePointerUp}
        onKeyUp={handleKeyUp}
        onClickCapture={handleClickCapture}
      >
        {children}
      </div>
      <ReaderContextMenu
        presentation={touchUI ? 'bottom-toolbar' : 'context-menu'}
        open={target !== null}
        anchorRect={target?.anchorRect ?? null}
        actions={actions}
        onClose={close}
      />
    </>
  )
}
