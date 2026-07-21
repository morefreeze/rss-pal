import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from 'react'
import { createPortal } from 'react-dom'
import type { ReaderContextAction } from './types'

const VIEWPORT_MARGIN = 8
const MENU_GAP = 8

type Position = { left: number; top: number }

type ReaderContextMenuProps = {
  open: boolean
  anchorRect: DOMRect | null
  actions: ReaderContextAction[]
  onClose: () => void
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), Math.max(min, max))
}

function menuPosition(anchor: DOMRect, menuWidth: number, menuHeight: number): Position {
  const maxLeft = window.innerWidth - menuWidth - VIEWPORT_MARGIN
  const left = clamp(anchor.left, VIEWPORT_MARGIN, maxLeft)
  const below = anchor.bottom + MENU_GAP
  const above = anchor.top - menuHeight - MENU_GAP
  const maxTop = window.innerHeight - menuHeight - VIEWPORT_MARGIN
  const top = below <= maxTop ? below : clamp(above, VIEWPORT_MARGIN, maxTop)
  return { left, top }
}

export function ReaderContextMenu({
  open,
  anchorRect,
  actions,
  onClose,
}: ReaderContextMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null)
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([])
  const [activeIndex, setActiveIndex] = useState(-1)
  const [busyActionID, setBusyActionID] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [position, setPosition] = useState<Position>({ left: VIEWPORT_MARGIN, top: VIEWPORT_MARGIN })

  const enabledIndices = actions.flatMap((action, index) => action.disabled ? [] : [index])

  const updatePosition = useCallback(() => {
    if (!anchorRect || !menuRef.current) return
    const menuRect = menuRef.current.getBoundingClientRect()
    setPosition(menuPosition(anchorRect, menuRect.width, menuRect.height))
  }, [anchorRect])

  useLayoutEffect(() => {
    if (!open) return
    updatePosition()
  }, [actions.length, open, updatePosition])

  useEffect(() => {
    if (!open) return
    window.addEventListener('resize', updatePosition)
    return () => window.removeEventListener('resize', updatePosition)
  }, [open, updatePosition])

  useEffect(() => {
    if (!open) {
      setActiveIndex(-1)
      setBusyActionID(null)
      setActionError(null)
      return
    }
    const first = enabledIndices[0] ?? -1
    setActiveIndex(first)
    setActionError(null)
  }, [open, actions]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!open || activeIndex < 0 || busyActionID) return
    itemRefs.current[activeIndex]?.focus()
  }, [activeIndex, busyActionID, open])

  useEffect(() => {
    if (!open) return
    const handleOutsidePointer = (event: PointerEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) onClose()
    }
    document.addEventListener('pointerdown', handleOutsidePointer)
    return () => document.removeEventListener('pointerdown', handleOutsidePointer)
  }, [onClose, open])

  const execute = useCallback(async (action: ReaderContextAction) => {
    if (action.disabled || busyActionID) return
    setBusyActionID(action.id)
    setActionError(null)
    try {
      await action.run()
      setBusyActionID(null)
      onClose()
    } catch {
      setBusyActionID(null)
      setActionError('操作失败，请重试')
    }
  }, [busyActionID, onClose])

  const moveFocus = (direction: 1 | -1) => {
    if (enabledIndices.length === 0) return
    const current = enabledIndices.indexOf(activeIndex)
    const next = current < 0
      ? 0
      : (current + direction + enabledIndices.length) % enabledIndices.length
    setActiveIndex(enabledIndices[next])
  }

  const handleKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape' || event.key === 'Tab') {
      if (event.key === 'Escape') event.preventDefault()
      onClose()
      return
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      moveFocus(event.key === 'ArrowDown' ? 1 : -1)
      return
    }
    if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault()
      const next = event.key === 'Home'
        ? enabledIndices[0]
        : enabledIndices[enabledIndices.length - 1]
      if (next !== undefined) setActiveIndex(next)
      return
    }
    if ((event.key === 'Enter' || event.key === ' ') && activeIndex >= 0) {
      event.preventDefault()
      void execute(actions[activeIndex])
    }
  }

  const retainSelection = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault()
  }

  if (!open || !anchorRect || actions.length === 0) return null

  return createPortal(
    <div
      ref={menuRef}
      role="menu"
      aria-busy={busyActionID !== null}
      className="reader-context-menu"
      onKeyDown={handleKeyDown}
      onPointerDown={retainSelection}
      style={{
        position: 'fixed',
        left: position.left,
        top: position.top,
        zIndex: 2200,
        minWidth: 176,
        padding: 6,
        border: '1px solid var(--border, #d8d8d8)',
        borderRadius: 10,
        background: 'var(--surface, #fff)',
        boxShadow: '0 10px 30px rgba(0, 0, 0, 0.16)',
      }}
    >
      {actions.map((action, index) => (
        <button
          key={action.id}
          ref={(node) => { itemRefs.current[index] = node }}
          type="button"
          role="menuitem"
          disabled={Boolean(action.disabled || busyActionID)}
          tabIndex={index === activeIndex ? 0 : -1}
          onFocus={() => setActiveIndex(index)}
          onClick={() => void execute(action)}
          style={{
            display: 'block',
            width: '100%',
            padding: '9px 12px',
            border: 0,
            borderRadius: 7,
            background: 'transparent',
            color: 'var(--fg, #222)',
            textAlign: 'left',
            cursor: action.disabled || busyActionID ? 'default' : 'pointer',
            opacity: action.disabled ? 0.45 : 1,
          }}
        >
          {action.label}
        </button>
      ))}
      {actionError && (
        <div
          role="alert"
          style={{ padding: '6px 12px 4px', color: '#b42318', fontSize: 12 }}
        >
          {actionError}
        </div>
      )}
    </div>,
    document.body,
  )
}
