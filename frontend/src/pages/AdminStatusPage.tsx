import { useEffect, useRef, useState } from 'react'

export default function AdminStatusPage() {
  const [version, setVersion] = useState(0)
  const frame = useRef<HTMLIFrameElement>(null)
  const observer = useRef<ResizeObserver | null>(null)
  useEffect(() => () => observer.current?.disconnect(), [])
  const resize = () => {
    observer.current?.disconnect()
    const element = frame.current
    const body = element?.contentDocument?.body
    if (!element || !body) return
    const syncHeight = () => { element.style.height = `${Math.max(640, body.scrollHeight)}px` }
    syncHeight()
    if (typeof ResizeObserver !== 'undefined') {
      observer.current = new ResizeObserver(syncHeight)
      observer.current.observe(body)
    }
  }
  return <div>
    <div className="flex-between" style={{ gap: 12, flexWrap: 'wrap', marginBottom: 16 }}>
      <h2 style={{ margin: 0 }}>服务状态</h2>
      <div className="flex gap-2" style={{ alignItems: 'center' }}>
        <button className="secondary" onClick={() => setVersion(value => value + 1)}>刷新状态页</button>
        <a href="/status" target="_blank" rel="noopener noreferrer">打开独立页面 ↗</a>
      </div>
    </div>
    <iframe key={version} ref={frame} src="/status" title="服务状态与 72 小时可用性" onLoad={resize}
      style={{ display: 'block', width: '100%', minHeight: 640, border: '1px solid var(--border)', borderRadius: 12 }} />
  </div>
}
