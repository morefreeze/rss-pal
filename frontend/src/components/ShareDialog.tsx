import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import {
  createArticleShare,
  listArticleShares,
  revokeArticleShare,
  type ArticleShareListItem,
} from '../api/client'

type ExpiryOption = '7d' | '30d' | 'custom' | 'permanent'

export type ShareDialogProps = {
  articleId: number
  articleTitle: string
  open: boolean
  onClose(): void
  onCopyXiaohongshu(): void
  onExportMarkdown(): void
}

function requestError(error: unknown, fallback: string): string {
  if (!error || typeof error !== 'object') return fallback
  const response = (error as { response?: { data?: { error?: unknown } } }).response
  return typeof response?.data?.error === 'string' && response.data.error.trim()
    ? response.data.error
    : fallback
}

function absoluteShareURL(row: ArticleShareListItem): string | null {
  if (!row.url) return null
  try {
    return new URL(row.url, window.location.origin).toString()
  } catch {
    return null
  }
}

function statusText(row: ArticleShareListItem): string {
  if (row.status === 'expired') return '已过期'
  if (row.status === 'revoked') return '已撤销'
  return row.legacy ? '旧版链接' : '有效'
}

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

function expiryValue(option: ExpiryOption, customExpiry: string): string | null | undefined {
  if (option === 'permanent') return null
  if (option === '7d' || option === '30d') {
    const days = option === '7d' ? 7 : 30
    return new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString()
  }
  if (!customExpiry) return undefined
  const date = new Date(customExpiry)
  return Number.isNaN(date.getTime()) || date.getTime() <= Date.now()
    ? undefined
    : date.toISOString()
}

export function ShareDialog({
  articleId,
  articleTitle,
  open,
  onClose,
  onCopyXiaohongshu,
  onExportMarkdown,
}: ShareDialogProps) {
  const [rows, setRows] = useState<ArticleShareListItem[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [expiry, setExpiry] = useState<ExpiryOption>('permanent')
  const [customExpiry, setCustomExpiry] = useState('')
  const [creating, setCreating] = useState(false)
  const [revoking, setRevoking] = useState<Set<string>>(() => new Set())
  const [fallbackURL, setFallbackURL] = useState('')
  const generationRef = useRef(0)
  const listReadyForRef = useRef<number | null>(null)
  const createRequestRef = useRef<Promise<ArticleShareListItem> | null>(null)
  const revokeRequestsRef = useRef(new Map<string, Promise<ArticleShareListItem>>())
  const dialogRef = useRef<HTMLElement>(null)
  const closeButtonRef = useRef<HTMLButtonElement>(null)
  const fallbackInputRef = useRef<HTMLInputElement>(null)
  const onCloseRef = useRef(onClose)

  useLayoutEffect(() => {
    onCloseRef.current = onClose
  }, [onClose])

  const closeDialog = useCallback(() => {
    setError('')
    setFallbackURL('')
    onCloseRef.current()
  }, [])

  useLayoutEffect(() => {
    if (!open) return
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    // Clear old action state before paint and before the fallback autofocus
    // effect can run for a newly mounted dialog surface.
    setError('')
    setFallbackURL('')
    closeButtonRef.current?.focus()
    return () => {
      if (opener?.isConnected) opener.focus()
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const generation = ++generationRef.current
    listReadyForRef.current = null
    setRows([])
    setLoading(true)
    setError('')
    setFallbackURL('')
    setCreating(false)
    setRevoking(new Set())
    createRequestRef.current = null
    revokeRequestsRef.current.clear()

    void listArticleShares(articleId).then((result) => {
      if (generationRef.current === generation) setRows(result)
    }).catch((cause: unknown) => {
      if (generationRef.current === generation) {
        setError(requestError(cause, '加载分享链接失败，请稍后重试'))
      }
    }).finally(() => {
      if (generationRef.current === generation) {
        listReadyForRef.current = articleId
        setLoading(false)
      }
    })

    return () => {
      if (generationRef.current === generation) {
        generationRef.current += 1
        listReadyForRef.current = null
      }
    }
  }, [articleId, open])

  useEffect(() => {
    if (!fallbackURL) return
    fallbackInputRef.current?.focus()
    fallbackInputRef.current?.select()
  }, [fallbackURL])

  useEffect(() => {
    if (!open) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        closeDialog()
        return
      }
      if (event.key !== 'Tab') return

      const dialog = dialogRef.current
      if (!dialog) return
      const focusable = Array.from(dialog.querySelectorAll<HTMLElement>(
        'button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ))
      if (focusable.length === 0) {
        event.preventDefault()
        dialog.focus()
        return
      }
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      const active = document.activeElement
      if (event.shiftKey && (active === first || !dialog.contains(active))) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && (active === last || !dialog.contains(active))) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [closeDialog, open])

  if (!open) {
    listReadyForRef.current = null
    return null
  }

  const listReady = listReadyForRef.current === articleId && !loading
  const selectedExpiry = expiryValue(expiry, customExpiry)
  const customInvalid = expiry === 'custom' && selectedExpiry === undefined

  const copyURL = async (
    row: ArticleShareListItem,
    created = false,
    generation = generationRef.current,
  ) => {
    const url = absoluteShareURL(row)
    if (!url) {
      if (generationRef.current === generation) {
        setFallbackURL('')
        setError(created ? '链接已创建，但未返回可复制的地址' : '该分享链接当前不可复制')
      }
      return
    }
    try {
      if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(url)
      if (generationRef.current === generation) {
        setFallbackURL('')
        setError('')
      }
    } catch {
      if (generationRef.current === generation) {
        setFallbackURL(url)
        setError(created ? '链接已创建，但复制失败，请手动复制链接' : '复制失败，请手动复制链接')
      }
    }
  }

  const createShare = async () => {
    const expiresAt = expiryValue(expiry, customExpiry)
    if (!listReady || expiresAt === undefined || createRequestRef.current) return
    setError('')
    setCreating(true)
    const generation = generationRef.current
    const request = createArticleShare(articleId, expiresAt)
    createRequestRef.current = request
    try {
      const created = await request
      if (generationRef.current !== generation || createRequestRef.current !== request) return
      setRows((current) => [created, ...current.filter((row) => row.id !== created.id)])
      await copyURL(created, true, generation)
    } catch (cause: unknown) {
      if (generationRef.current !== generation || createRequestRef.current !== request) return
      const status = (cause as { response?: { status?: number } })?.response?.status
      setError(status === 409
        ? '文章正文尚未准备完成'
        : requestError(cause, '创建分享链接失败，请稍后重试'))
    } finally {
      if (createRequestRef.current === request) createRequestRef.current = null
      if (generationRef.current === generation) setCreating(false)
    }
  }

  const revokeShare = async (row: ArticleShareListItem) => {
    if (revokeRequestsRef.current.has(row.id)) return
    setError('')
    const generation = generationRef.current
    const request = revokeArticleShare(articleId, row.id)
    revokeRequestsRef.current.set(row.id, request)
    setRevoking((current) => new Set(current).add(row.id))
    try {
      const updated = await request
      if (generationRef.current !== generation || revokeRequestsRef.current.get(row.id) !== request) return
      setRows((current) => current.map((item) => item.id === updated.id ? updated : item))
    } catch (cause: unknown) {
      if (generationRef.current === generation && revokeRequestsRef.current.get(row.id) === request) {
        setError(requestError(cause, '撤销分享链接失败，请稍后重试'))
      }
    } finally {
      if (revokeRequestsRef.current.get(row.id) === request) revokeRequestsRef.current.delete(row.id)
      if (generationRef.current === generation) {
        setRevoking((current) => {
          const next = new Set(current)
          next.delete(row.id)
          return next
        })
      }
    }
  }

  const shareToX = (row: ArticleShareListItem) => {
    const url = absoluteShareURL(row)
    if (!url) {
      setError('该分享链接当前不可用')
      return
    }
    const intent = new URL('https://twitter.com/intent/tweet')
    intent.searchParams.set('text', articleTitle)
    intent.searchParams.set('url', url)
    window.open(intent.toString(), '_blank', 'noopener,noreferrer')
  }

  return (
    <div className="share-dialog-overlay" onMouseDown={(event) => {
      if (event.target === event.currentTarget) closeDialog()
    }}>
      <section
        ref={dialogRef}
        className="share-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="share-dialog-title"
        tabIndex={-1}
      >
        <header className="share-dialog-header">
          <h2 id="share-dialog-title">管理分享链接</h2>
          <button ref={closeButtonRef} type="button" className="btn-ghost" aria-label="关闭" onClick={closeDialog}>×</button>
        </header>

        <div className="share-dialog-body">
          <fieldset className="share-expiry-options" disabled={!listReady || creating}>
            <legend>链接有效期</legend>
            <label><input type="radio" name="share-expiry" checked={expiry === 'permanent'} onChange={() => setExpiry('permanent')} />永久</label>
            <label><input type="radio" name="share-expiry" checked={expiry === '7d'} onChange={() => setExpiry('7d')} />7 天</label>
            <label><input type="radio" name="share-expiry" checked={expiry === '30d'} onChange={() => setExpiry('30d')} />30 天</label>
            <label><input type="radio" name="share-expiry" checked={expiry === 'custom'} onChange={() => setExpiry('custom')} />自定义</label>
          </fieldset>
          {expiry === 'custom' && (
            <div className="share-custom-expiry">
              <label htmlFor="share-custom-expiry">到期时间</label>
              <input
                id="share-custom-expiry"
                type="datetime-local"
                value={customExpiry}
                onChange={(event) => setCustomExpiry(event.target.value)}
                aria-invalid={customInvalid}
                disabled={!listReady || creating}
              />
              {customInvalid && customExpiry && <span className="text-muted text-sm">请选择未来时间</span>}
            </div>
          )}
          <button
            type="button"
            onClick={() => void createShare()}
            disabled={!listReady || creating || customInvalid}
          >
            {creating ? '创建中…' : '创建新链接'}
          </button>

          {error && <p className="share-dialog-error" role="alert">{error}</p>}
          {fallbackURL && (
            <div className="share-copy-fallback">
              <label htmlFor="share-copy-fallback">手动复制链接</label>
              <input
                ref={fallbackInputRef}
                id="share-copy-fallback"
                type="text"
                value={fallbackURL}
                readOnly
                onFocus={(event) => event.currentTarget.select()}
              />
            </div>
          )}

          <div className="share-list-section">
            <h3>已有链接</h3>
            {loading ? (
              <p className="text-muted">正在加载分享链接…</p>
            ) : rows.length === 0 ? (
              <p className="text-muted">还没有分享链接</p>
            ) : (
              <ul className="share-list">
                {rows.map((row) => {
                  const revocable = row.status === 'active'
                  const copyable = revocable && !row.legacy && Boolean(absoluteShareURL(row))
                  return (
                    <li key={row.id}>
                      <div className="share-list-meta">
                        <code>{row.id}</code>
                        <span className={`share-status share-status-${row.status}`}>{statusText(row)}</span>
                        {row.legacy && row.status !== 'active' && <span className="share-status">旧版链接</span>}
                        <span className="text-muted text-sm">创建于 {formatDate(row.created_at)}</span>
                        <span className="text-muted text-sm">
                          {row.expires_at ? `到期于 ${formatDate(row.expires_at)}` : '永久有效'}
                        </span>
                      </div>
                      {(copyable || revocable) && (
                        <div className="share-list-actions">
                          {copyable && (
                            <>
                              <button type="button" className="secondary btn-sm" onClick={() => void copyURL(row)}>复制链接</button>
                              <button type="button" className="secondary btn-sm" onClick={() => shareToX(row)}>分享到 X</button>
                            </>
                          )}
                          {revocable && (
                            <button
                              type="button"
                              className="secondary btn-sm"
                              disabled={revoking.has(row.id)}
                              onClick={() => void revokeShare(row)}
                            >
                              {revoking.has(row.id) ? '撤销中…' : '撤销'}
                            </button>
                          )}
                        </div>
                      )}
                    </li>
                  )
                })}
              </ul>
            )}
          </div>

          <div className="share-dialog-secondary-actions">
            <button type="button" className="secondary" onClick={onCopyXiaohongshu}>小红书复制</button>
            <button type="button" className="secondary" onClick={onExportMarkdown}>导出 Markdown</button>
          </div>
        </div>
      </section>
    </div>
  )
}

export default ShareDialog
