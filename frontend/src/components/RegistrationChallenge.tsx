import { useEffect, useRef, useState } from 'react'

type TurnstileOptions = {
  sitekey: string
  action: string
  callback: (token: string) => void
  'expired-callback': () => void
  'error-callback': () => void
}
declare global {
  interface Window {
    turnstile?: {
      render: (container: HTMLElement, options: TurnstileOptions) => string
      remove: (widgetId: string) => void
    }
  }
}

let scriptLoading: Promise<void> | undefined
function loadTurnstile(): Promise<void> {
  if (window.turnstile) return Promise.resolve()
  if (scriptLoading) return scriptLoading
  scriptLoading = new Promise<void>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
    script.async = true
    const fail = () => {
      window.clearTimeout(timer)
      script.remove()
      scriptLoading = undefined
      reject(new Error('verification unavailable'))
    }
    const timer = window.setTimeout(fail, 15000)
    script.onload = () => {
      window.clearTimeout(timer)
      if (window.turnstile) resolve()
      else fail()
    }
    script.onerror = fail
    document.head.appendChild(script)
  })
  return scriptLoading
}

export default function RegistrationChallenge({siteKey, onVerify}: {siteKey: string; onVerify: (token: string) => void}) {
  const container = useRef<HTMLDivElement>(null)
  const [error, setError] = useState(false)
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let disposed = false
    let widget: string | undefined
    onVerify('')
    setError(false)
    loadTurnstile().then(() => {
      if (disposed || !container.current || !window.turnstile) return
      widget = window.turnstile.render(container.current, {
        sitekey: siteKey,
        action: 'signup',
        callback: token => { if (!disposed) { setError(false); onVerify(token) } },
        'expired-callback': () => { if (!disposed) onVerify('') },
        'error-callback': () => { if (!disposed) { onVerify(''); setError(true) } },
      })
    }).catch(() => { if (!disposed) { onVerify(''); setError(true) } })
    return () => {
      disposed = true
      if (widget !== undefined) window.turnstile?.remove(widget)
    }
  }, [siteKey, onVerify, attempt])

  return <div className="mb-2">
    <div ref={container} aria-label="注册人机验证" />
    {error && <div role="alert" className="text-sm">
      人机验证加载失败，请检查网络后重试。
      <button type="button" className="secondary" onClick={() => setAttempt(value => value + 1)}>重新验证</button>
    </div>}
  </div>
}
