import { useEffect, useMemo, useRef, useState } from 'react'
import {
  getExploreSources,
  subscribeExploreSource,
  subscribeExploreSources,
  type ExploreSource,
} from '../api/client'
import { useBreakpoint } from '../hooks/useBreakpoint'

interface Props {
  onSubscribed?: (sourceIDs: number[]) => void
}

function healthLabel(source: ExploreSource) {
  if (source.validation_status !== 'valid') return '待校验'
  if (source.health_score == null) return '健康'
  return source.health_score >= 0.8 ? '健康' : source.health_score >= 0.5 ? '一般' : '需关注'
}

export default function ExploreSourceDrawer({ onSubscribed }: Props) {
  const breakpoint = useBreakpoint()
  const [sources, setSources] = useState<ExploreSource[]>([])
  const [open, setOpen] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(() => new Set())
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const handleRef = useRef<HTMLButtonElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const drawerRef = useRef<HTMLElement>(null)
  const wasOpenRef = useRef(false)

  useEffect(() => {
    let active = true
    getExploreSources()
      .then(items => {
        if (!active) return
        const next = items ?? []
        setSources(next)
        setSelected(new Set(next.filter(item => item.selected && !item.is_subscribed).map(item => item.id)))
      })
      .catch(() => { if (active) setSources([]) })
    return () => { active = false }
  }, [])

  useEffect(() => {
    if (!open) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
      if (event.key !== 'Tab') return
      const focusable = Array.from(drawerRef.current?.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ) ?? [])
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    closeRef.current?.focus()
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      document.body.style.overflow = previousOverflow
    }
  }, [open])

  useEffect(() => {
    if (!open && wasOpenRef.current) handleRef.current?.focus()
    wasOpenRef.current = open
  }, [open])

  const selectedIDs = useMemo(
    () => sources.filter(source => selected.has(source.id) && !source.is_subscribed).map(source => source.id),
    [selected, sources],
  )

  const markSubscribed = (sourceIDs: number[]) => {
    const idSet = new Set(sourceIDs)
    setSources(current => current.map(source => idSet.has(source.id)
      ? { ...source, is_subscribed: true, selected: false }
      : source))
    setSelected(current => {
      const next = new Set(current)
      sourceIDs.forEach(id => next.delete(id))
      return next
    })
    onSubscribed?.(sourceIDs)
  }

  const subscribeOne = async (source: ExploreSource) => {
    setPending(true)
    setError(null)
    try {
      await subscribeExploreSource(source.id)
      markSubscribed([source.id])
      setOpen(false)
    } catch {
      setError('订阅失败，请重试')
    } finally {
      setPending(false)
    }
  }

  const subscribeSelected = async () => {
    if (selectedIDs.length === 0) return
    setPending(true)
    setError(null)
    try {
      await subscribeExploreSources(selectedIDs)
      markSubscribed(selectedIDs)
      setOpen(false)
    } catch {
      setError('批量订阅失败，请重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <>
      <button
        ref={handleRef}
        type="button"
        className={`explore-source-handle explore-source-handle--${breakpoint === 'phone' ? 'mobile' : 'desktop'}`}
        aria-label={`查看 ${sources.length} 个候选源`}
        aria-expanded={open}
        aria-controls="explore-source-drawer"
        onClick={() => { setError(null); setOpen(true) }}
      >
        <span aria-hidden="true">☰</span>
        <span>{sources.length} 个候选源</span>
      </button>

      {open && (
        <>
          <div
            className="explore-drawer-backdrop"
            data-testid="explore-drawer-backdrop"
            onPointerDown={() => setOpen(false)}
          />
          <aside
            ref={drawerRef}
            id="explore-source-drawer"
            role="dialog"
            aria-modal="true"
            aria-label="候选订阅源"
            data-placement={breakpoint === 'phone' ? 'bottom' : 'right'}
            className={`explore-source-drawer explore-source-drawer--${breakpoint === 'phone' ? 'bottom' : 'right'}`}
          >
            <header className="explore-source-drawer__header">
              <div>
                <h2>候选订阅源</h2>
                <p>选择你想正式订阅的来源</p>
              </div>
              <button ref={closeRef} type="button" className="btn-ghost" aria-label="关闭候选订阅源" onClick={() => setOpen(false)}>×</button>
            </header>

            {error && <div role="alert" className="explore-inline-error">{error}</div>}

            <div className="explore-source-list">
              {sources.map(source => (
                <section key={source.id} className="explore-source-item">
                  <div className="explore-source-item__heading">
                    <label>
                      <input
                        type="checkbox"
                        aria-label={`选择 ${source.title}`}
                        checked={selected.has(source.id) && !source.is_subscribed}
                        disabled={source.is_subscribed || pending}
                        onChange={event => setSelected(current => {
                          const next = new Set(current)
                          if (event.target.checked) next.add(source.id)
                          else next.delete(source.id)
                          return next
                        })}
                      />
                      <strong>{source.title}</strong>
                    </label>
                    <span className="explore-health">{healthLabel(source)}</span>
                  </div>
                  <div className="explore-source-meta">
                    <span>{source.topic || '综合'}</span>
                    <span>最近 {source.recent_article_count} 篇</span>
                  </div>
                  <p>{source.reason}</p>
                  {source.is_subscribed ? (
                    <button type="button" className="btn-ghost" disabled aria-label="已订阅">已订阅</button>
                  ) : (
                    <button
                      type="button"
                      className="secondary"
                      disabled={pending}
                      aria-label={`订阅 ${source.title}`}
                      onClick={() => void subscribeOne(source)}
                    >订阅</button>
                  )}
                </section>
              ))}
              {sources.length === 0 && <p className="text-muted">当前没有可管理的候选源</p>}
            </div>

            <footer className="explore-source-drawer__footer">
              <button type="button" disabled={pending || selectedIDs.length === 0} onClick={() => void subscribeSelected()}>
                {pending ? '订阅中…' : `订阅已选 ${selectedIDs.length} 个`}
              </button>
            </footer>
          </aside>
        </>
      )}
    </>
  )
}
