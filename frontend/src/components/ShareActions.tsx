import { useEffect, useRef, useState } from 'react'
import { QRCodeSVG } from 'qrcode.react'
import { buildXIntentURL, buildXPostText, type XPostInput } from '../utils/xShare'

type Props = XPostInput & { copyLink?: boolean; xAsButton?: boolean }

export default function ShareActions({ copyLink = true, xAsButton = false, ...input }: Props) {
  const text = buildXPostText(input)
  const [message, setMessage] = useState('')
  const [fallback, setFallback] = useState('')
  const [wechat, setWechat] = useState(false)
  const [busy, setBusy] = useState(false)
  const mounted = useRef(true)
  const sharing = useRef(false)
  const textarea = useRef<HTMLTextAreaElement>(null)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  useEffect(() => { if (fallback) { textarea.current?.focus(); textarea.current?.select() } }, [fallback])

  const copy = async (value: string, linkOnly = false, hint = '') => {
    try {
      if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(value)
      if (!mounted.current) return
      setFallback('')
      setMessage(hint || (linkOnly ? '链接已复制' : '分享文案已复制'))
    } catch {
      if (!mounted.current) return
      setFallback(value)
      setMessage('复制失败，请长按或选中文案手动复制')
    }
  }

  const share = async (toWechat: boolean) => {
    if (sharing.current) return
    sharing.current = true
    setBusy(true)
    setMessage('')
    setFallback('')
    setWechat(toWechat)
    try {
      if (!navigator.share) throw new Error('native share unavailable')
      // Do not await anything before share: iOS requires the original click activation.
      await navigator.share({
        title: text.split('\n')[0],
        text: text.endsWith(input.shareURL) ? text.slice(0, -input.shareURL.length).trim() : text,
        url: input.shareURL,
      })
    } catch (error) {
      if (!mounted.current || (error as { name?: string })?.name === 'AbortError') return
      setWechat(toWechat)
      await copy(text, false, toWechat ? '文案已复制，请切换到微信粘贴发送' : '当前无法使用系统分享，分享文案已复制')
    } finally {
      sharing.current = false
      if (mounted.current) setBusy(false)
    }
  }

  const intent = buildXIntentURL(text)
  return <div className="social-share">
    <div className="social-share-buttons">
      <button type="button" className="secondary btn-sm" onClick={() => void copy(text)}>复制分享文案</button>
      {copyLink && <button type="button" className="secondary btn-sm" onClick={() => void copy(input.shareURL, true)}>复制链接</button>}
      {xAsButton
        ? <button type="button" className="secondary btn-sm" onClick={() => window.open(intent, '_blank', 'noopener,noreferrer')}>分享到 X</button>
        : <a className="public-reader-x-cta" href={intent} target="_blank" rel="noopener noreferrer">分享到 X</a>}
      <button type="button" className="secondary btn-sm" disabled={busy} onClick={() => void share(true)}>分享到微信</button>
      <button type="button" className="secondary btn-sm" disabled={busy} onClick={() => void share(false)}>更多分享</button>
    </div>
    {message && <p role="status" className="text-muted text-sm">{message}</p>}
    {wechat && <div className="wechat-share-help">
      <QRCodeSVG value={input.shareURL} size={180} marginSize={4} role="img" aria-label="微信扫码阅读文章" />
      <p>用微信扫一扫，打开文章后分享给好友；手机上可复制文案，切换到微信粘贴发送。</p>
      {/MicroMessenger/i.test(navigator.userAgent) && <p>也可以在微信内打开文章后，点击右上角「…」分享。</p>}
    </div>}
    {fallback && <textarea ref={textarea} aria-label="手动复制分享文案" rows={4} readOnly value={fallback} onFocus={event => event.currentTarget.select()} />}
  </div>
}
