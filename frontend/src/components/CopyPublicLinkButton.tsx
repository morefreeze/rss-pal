import { useEffect, useRef, useState } from 'react'
import { createArticleShare, listArticleShares, type ArticleShareListItem } from '../api/client'

function publicURL(row: ArticleShareListItem): string | null {
  if (row.status !== 'active' || row.legacy || !row.url) return null
  if (row.expires_at && !(Date.parse(row.expires_at) > Date.now())) return null
  try {
    const url = new URL(row.url, window.location.origin)
    return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password ? url.href : null
  } catch { return null }
}

export default function CopyPublicLinkButton({ articleId }: { articleId: number }) {
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [fallback, setFallback] = useState('')
  const pending = useRef(false)
  const mounted = useRef(true)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  useEffect(() => { if (fallback) { input.current?.focus(); input.current?.select() } }, [fallback])

  const copy = async () => {
    if (pending.current) return
    pending.current = true
    setBusy(true)
    setMessage('')
    setError('')
    setFallback('')
    const urlRequest = (async () => {
      const rows = await listArticleShares(articleId)
      if (!mounted.current) throw new Error('unmounted')
      const existing = rows.map(publicURL).find((url): url is string => Boolean(url))
      if (existing) return existing
      const url = publicURL(await createArticleShare(articleId, null))
      if (!mounted.current) throw new Error('unmounted')
      if (!url) throw new Error('missing share URL')
      return url
    })()

    let clipboardRequest: Promise<void>
    try {
      if (navigator.clipboard?.write && typeof ClipboardItem !== 'undefined') {
        // Start clipboard access in the click gesture; Safari can resolve the URL later.
        const data = urlRequest.then(url => new Blob([url], { type: 'text/plain' }))
        void data.catch(() => {}) // Some browsers reject before consuming the promised data.
        clipboardRequest = navigator.clipboard.write([new ClipboardItem({ 'text/plain': data })])
      } else {
        clipboardRequest = urlRequest.then(url => {
          if (!mounted.current || !navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
          return navigator.clipboard.writeText(url)
        })
      }
    } catch (cause) { clipboardRequest = Promise.reject(cause) }

    const [link, clipboard] = await Promise.allSettled([urlRequest, clipboardRequest])
    pending.current = false
    if (!mounted.current) return
    setBusy(false)
    if (link.status === 'rejected') {
      const status = (link.reason as { response?: { status?: number } })?.response?.status
      setError(status === 409 ? '文章正文尚未准备完成，请稍后重试' : '获取公开分享链接失败，请重试')
    } else if (clipboard.status === 'rejected') {
      setFallback(link.value)
      setError('复制失败，请长按或选中链接手动复制')
    } else {
      setMessage('公开分享链接已复制')
    }
  }

  return <div className="article-copy-public-link">
    <button type="button" className="secondary" style={{ fontSize: 13 }} disabled={busy}
      title="复制公开分享链接" onClick={() => void copy()}>{busy ? '复制中…' : '复制链接'}</button>
    {message && <span role="status" className="text-muted text-sm">{message}</span>}
    {error && <span role="alert" className="text-sm">{error}</span>}
    {fallback && <input ref={input} aria-label="公开分享链接" readOnly value={fallback}
      onFocus={event => event.currentTarget.select()} />}
  </div>
}
