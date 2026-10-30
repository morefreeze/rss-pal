import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import type { User } from '../App'
import { markNicknamePrompted, setNickname } from '../api/client'

// Nested dialogs may unmount in either order. Restore scrolling after the last one.
let scrollLocks = 0
let previousOverflow = ''

export function AccountSetupModal({ children, title, alert = false, onClose }: { children: ReactNode; title: string; alert?: boolean; onClose?: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const titleId = useId()
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    if (scrollLocks++ === 0) {
      previousOverflow = document.body.style.overflow
      document.body.style.overflow = 'hidden'
    }
    ref.current?.querySelector<HTMLElement>('input, button')?.focus()
    const onKey = (event: KeyboardEvent) => {
      const modals = document.querySelectorAll('.nickname-modal')
      if (modals[modals.length - 1] !== ref.current) return
      if (event.key === 'Escape') { event.preventDefault(); onClose?.() }
      if (event.key !== 'Tab') return
      const elements = ref.current?.querySelectorAll<HTMLElement>('input:not(:disabled), button:not(:disabled)')
      if (!elements?.length) return
      const first = elements[0], last = elements[elements.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', onKey)
    return () => { document.removeEventListener('keydown', onKey); if (--scrollLocks === 0) document.body.style.overflow = previousOverflow; previous?.focus() }
  }, [onClose])
  return createPortal(<div className="share-dialog-overlay" style={{ zIndex: alert ? 2200 : 2100 }}>
    <div ref={ref} className="share-dialog nickname-modal" role={alert ? 'alertdialog' : 'dialog'} aria-modal="true" aria-labelledby={titleId} style={{ maxWidth: 420, padding: 24, overflowY: 'auto' }}>
      <h3 id={titleId} className="mb-2">{title}</h3>{children}
    </div>
  </div>, document.body)
}

export default function NicknameSetup({ user, onSaved }: { user: User; onSaved: (user: User) => void }) {
  const qqName = user.qq_nickname?.trim() || ''
  const [name, setName] = useState<string | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const inputId = useId()
  const pending = useRef(false)
  const alive = useRef(true)
  useEffect(() => { alive.current = true; return () => { alive.current = false } }, [])
  const trimmed = (name ?? qqName).trim()
  const save = async () => {
    if (pending.current) return
    pending.current = true
    setSaving(true); setError('')
    try {
      const updated = await setNickname(trimmed)
      if (alive.current) { setConfirming(false); onSaved(updated) }
    } catch (e: unknown) {
      if (alive.current) {
        const message = (e as { response?: { data?: { error?: string } } }).response?.data?.error
        setError(message || '昵称保存失败，请重试')
        setConfirming(false)
      }
    } finally {
      pending.current = false
      if (alive.current) setSaving(false)
    }
  }
  if (user.nickname) return <><p>昵称：{user.nickname}</p><p className="text-muted text-sm">昵称已设置，无法再次修改。</p></>
  return <>
    <form onSubmit={e => {
      e.preventDefault()
      if (qqName && trimmed === qqName) return
      if (!trimmed || [...trimmed].length > 32 || /[\p{Cc}\p{Cf}]/u.test(trimmed)) { setError('昵称须为 1–32 个字符，不能包含控制字符'); return }
      setError(''); setConfirming(true)
    }}>
      <p className="mb-2 text-sm" style={{ color: 'var(--danger, #dc2626)' }}>{qqName ? '默认使用 QQ 昵称，可修改一次；保存后无法再次修改。' : '昵称只能设置一次，确认后无法修改。'}</p>
      <label htmlFor={inputId}>昵称</label>
      <p className="mb-2 text-sm text-muted">不能使用冒充官方、辱骂、色情等不当昵称。</p>
      <input id={inputId} value={name ?? qqName} onChange={e => setName(e.target.value)} placeholder="给自己起一个昵称" disabled={saving} style={{ width: '100%', margin: '8px 0' }} />
      {error && <p role="alert" className="mb-1" style={{ color: 'var(--danger, #dc2626)' }}>{error}</p>}
      <button type="submit" disabled={saving || !trimmed || trimmed === qqName}>{saving ? '保存中…' : '确认昵称'}</button>
    </form>
    {confirming && <AccountSetupModal title="确认后无法修改昵称" alert onClose={() => { if (!pending.current) setConfirming(false) }}>
      <p className="mb-2">昵称将永久设置为「{trimmed}」，保存后无法再次修改，请确认无误。</p>
      <div className="flex gap-2" style={{ flexWrap: 'wrap' }}>
        <button type="button" className="secondary" disabled={saving} onClick={() => setConfirming(false)}>返回修改</button>
        <button type="button" disabled={saving} onClick={() => void save()}>{saving ? '保存中…' : '确认并永久保存'}</button>
      </div>
    </AccountSetupModal>}
  </>
}

export function NicknameOnboarding({ user, onSaved }: { user: User; onSaved: (user: User) => void }) {
  const hasNickname = Boolean(user.nickname || user.qq_nickname?.trim())
  const key = `rsspal:nickname-skipped:${user.id}`
  const seenKey = `rsspal:nickname-prompted:${user.id}`
  const [visibleFor, setVisibleFor] = useState<number | null>(null)
  useEffect(() => {
    if (!user.needs_nickname || hasNickname) return
    const seen = localStorage.getItem(seenKey) || sessionStorage.getItem(key)
    if (!seen) setVisibleFor(user.id)
    localStorage.setItem(seenKey, '1')
    // This endpoint is idempotent. Retry when a stale cached profile still
    // requests the prompt, without showing it again after a failed request.
    void markNicknamePrompted().catch(() => {})
  }, [user.id, user.needs_nickname, hasNickname, key, seenKey])
  if (visibleFor !== user.id || hasNickname) return null
  return <AccountSetupModal title="欢迎，设置你的昵称">
    <p className="mb-2 text-sm">昵称会显示在账号菜单中。你也可以稍后到“设置 → 账号”中设置。</p>
    <NicknameSetup key={user.id} user={user} onSaved={onSaved} />
    <button type="button" className="secondary" style={{ marginTop: 12 }} onClick={() => { sessionStorage.setItem(key, '1'); setVisibleFor(null) }}>稍后设置</button>
  </AccountSetupModal>
}
