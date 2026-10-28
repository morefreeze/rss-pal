import {
  useEffect,
  useRef,
  useState,
  type ReactNode,
  type TouchEvent,
} from 'react'
import type { ReadDirection } from '../api/client'

const nativeInteractionSelector = 'button,a,input,select,textarea,video,audio,[role="menu"],[role="button"],[role="link"],[contenteditable]:not([contenteditable="false"])'
function isNativeInteraction(target: EventTarget | null) {
  const element = target instanceof Element ? target : target instanceof Node ? target.parentElement : null
  return Boolean(element?.closest(nativeInteractionSelector))
}

interface Props {
  children: ReactNode
  onMarkRead: (direction: ReadDirection) => void
  allowRange?: boolean
  disabled?: boolean
}

// Touch interaction lives outside ArticleCard so normal and tweet cards share
// thresholds, scroll cancellation, and suppression of synthesized clicks.
export default function ArticleReadSurface({
  children,
  onMarkRead,
  allowRange = true,
  disabled = false,
}: Props) {
  const [menu, setMenu] = useState(false)
  const [drag, setDrag] = useState(0)
  const root = useRef<HTMLDivElement>(null)
  const gesture = useRef<{
    x: number
    y: number
    axis: 'pending' | 'horizontal'
    dx: number
  } | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const suppressUntil = useRef(0)
  const consumed = useRef(false)
  // Browsers can cancel touch delivery before firing their native menu.
  // Keep the touch origin until a new mouse/keyboard interaction, independent
  // of the read timer and gesture cancellation.
  const suppressNativeMenu = useRef(false)
  const clearTimer = () => {
    if (timer.current) clearTimeout(timer.current)
    timer.current = null
  }
  const cancel = () => {
    clearTimer()
    gesture.current = null
    setDrag(0)
  }
  useEffect(() => () => clearTimer(), [])
  useEffect(() => {
    if (!menu) return
    const outside = (e: Event) => {
      if (!root.current?.contains(e.target as Node)) setMenu(false)
    }
    const escape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setMenu(false)
        root.current
          ?.querySelector<HTMLButtonElement>('[aria-haspopup]')
          ?.focus()
      }
    }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', escape)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('keydown', escape)
    }
  }, [menu])
  useEffect(() => {
    if (disabled) cancel()
  }, [disabled])
  const mark = (direction: ReadDirection) => {
    setMenu(false)
    onMarkRead(direction)
  }
  const onStart = (e: TouchEvent) => {
    cancel()
    consumed.current = false
    suppressNativeMenu.current = false
    if (
      disabled ||
      menu ||
      e.touches.length !== 1 ||
      isNativeInteraction(e.target)
    )
      return
    suppressNativeMenu.current = true
    const t = e.touches[0]
    gesture.current = { x: t.clientX, y: t.clientY, axis: 'pending', dx: 0 }
    timer.current = setTimeout(() => {
      gesture.current = null
      consumed.current = true
      suppressUntil.current = Date.now() + 900
      mark('single')
    }, 550)
  }
  const onMove = (e: TouchEvent) => {
    const g = gesture.current
    if (!g) return
    if (e.touches.length !== 1) {
      cancel()
      return
    }
    const dx = e.touches[0].clientX - g.x,
      dy = e.touches[0].clientY - g.y
    if (g.axis === 'pending') {
      if (Math.abs(dx) < 10 && Math.abs(dy) < 10) return
      clearTimer()
      if (Math.abs(dy) >= Math.abs(dx)) {
        cancel()
        return
      }
      g.axis = 'horizontal'
    }
    g.dx = dx
    setDrag(Math.max(-90, Math.min(90, dx)))
  }
  const onEnd = (e: TouchEvent) => {
    const g = gesture.current
    if (consumed.current) {
      suppressUntil.current = Date.now() + 900
      consumed.current = false
    }
    clearTimer()
    if (g && e.changedTouches.length === 1) {
      const dx = e.changedTouches[0].clientX - g.x,
        dy = e.changedTouches[0].clientY - g.y
      if (g.axis === 'horizontal') {
        suppressUntil.current = Date.now() + 900
        if (Math.abs(dx) >= 72 && Math.abs(dx) > Math.abs(dy) * 1.4) {
          if (dx < 0) mark('single')
          else setMenu(true)
        }
      }
    }
    gesture.current = null
    setDrag(0)
  }
  return (
    <div
      ref={root}
      className="article-read-surface"
      onTouchStart={onStart}
      onTouchMove={onMove}
      onTouchEnd={onEnd}
      onTouchCancel={cancel}
      onPointerDownCapture={(e) => {
        if (e.pointerType === 'mouse') suppressNativeMenu.current = false
      }}
      onKeyDownCapture={() => { suppressNativeMenu.current = false }}
      onContextMenuCapture={(e) => {
        if (!isNativeInteraction(e.target) && suppressNativeMenu.current) {
          e.preventDefault()
        }
      }}
      onClickCapture={(e) => {
        if (
          Date.now() < suppressUntil.current &&
          !(e.target as Element).closest('button,[role="menu"]')
        ) {
          e.preventDefault()
          e.stopPropagation()
        }
      }}
    >
      {drag !== 0 && (
        <div className="article-read-swipe-hint" aria-hidden>
          {drag < 0 ? '✓ 标记这篇已读' : '选择已读范围 →'}
        </div>
      )}
      <div style={{ transform: drag ? `translateX(${drag}px)` : undefined }}>
        {children}
      </div>
      <div
        className="article-read-controls"
        onClick={(e) => e.stopPropagation()}
      >
        <button
          type="button"
          className="btn-ghost"
          aria-label="文章已读操作"
          title="已读操作 · 左滑或长按标记这篇，右滑选择范围"
          aria-haspopup="menu"
          aria-expanded={menu}
          disabled={disabled}
          onClick={() => setMenu((v) => !v)}
        >
          ⋯
        </button>
        {menu && (
          <div className="article-read-menu" role="menu" aria-label="标记已读">
            <button
              type="button"
              role="menuitem"
              disabled={disabled}
              onClick={() => mark('single')}
            >
              这篇标记已读
            </button>
            {allowRange && (
              <>
                <button
                  type="button"
                  role="menuitem"
                  disabled={disabled}
                  onClick={() => mark('above')}
                >
                  这篇及以上标记已读
                </button>
                <button
                  type="button"
                  role="menuitem"
                  disabled={disabled}
                  onClick={() => mark('below')}
                >
                  这篇及以下标记已读
                </button>
                <span className="text-muted text-sm">
                  按当前排序和筛选，包含未加载文章
                </span>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
