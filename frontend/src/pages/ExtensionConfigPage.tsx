import { useEffect, useState } from 'react'

type Status = 'pending' | 'ok' | 'no-extension' | 'bad-url'

export default function ExtensionConfigPage() {
  const [status, setStatus] = useState<Status>('pending')

  useEffect(() => {
    const raw = window.location.hash.replace(/^#/, '')
    const params = new URLSearchParams(raw)
    if (!params.get('token') || !params.get('serverUrl')) {
      setStatus('bad-url')
      return
    }

    const onMsg = (e: MessageEvent) => {
      // Content scripts post from an isolated world — don't require e.source
      // identity. Just trust the type marker.
      if (e.data && e.data.type === 'RSS_PAL_EXTENSION_CONFIGURED') {
        setStatus('ok')
      }
    }
    window.addEventListener('message', onMsg)

    const timer = window.setTimeout(() => {
      setStatus(s => (s === 'pending' ? 'no-extension' : s))
    }, 4000)

    return () => {
      window.removeEventListener('message', onMsg)
      window.clearTimeout(timer)
    }
  }, [])

  return (
    <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
      <div style={{ maxWidth: 480, width: '100%', padding: 24, border: '1px solid var(--border, #e5e7eb)', borderRadius: 8, background: 'var(--card-bg, #fff)' }}>
        <h2 style={{ margin: '0 0 12px' }}>🧩 配置 RSS Pal 扩展</h2>
        {status === 'pending' && (
          <p className="text-muted">正在配置扩展…</p>
        )}
        {status === 'ok' && (
          <>
            <p style={{ color: 'var(--success, #16a34a)', fontSize: 16, margin: '0 0 8px' }}>✅ 扩展已配置完成</p>
            <p className="text-muted text-sm" style={{ margin: 0 }}>可关闭此标签页，回到任意网页点击扩展图标即可抓取。</p>
          </>
        )}
        {status === 'no-extension' && (
          <>
            <p style={{ color: 'var(--warning, #d97706)', fontSize: 16, margin: '0 0 8px' }}>⚠️ 未检测到扩展响应</p>
            <ul className="text-sm text-muted" style={{ margin: '0 0 0 18px', padding: 0, lineHeight: 1.7 }}>
              <li>请确认 RSS Pal 扩展已安装并启用</li>
              <li>如果刚装上还没生效，<a href="" onClick={(e) => { e.preventDefault(); window.location.reload() }}>刷新本页</a> 重试</li>
              <li>或回到设置页用手动方式（复制 token 粘贴到扩展）</li>
            </ul>
          </>
        )}
        {status === 'bad-url' && (
          <p style={{ color: 'var(--error, #dc2626)' }}>❌ URL 缺少 token 或 serverUrl 参数</p>
        )}
      </div>
    </div>
  )
}
