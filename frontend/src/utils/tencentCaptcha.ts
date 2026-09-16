type CaptchaResult = {ret: number; ticket?: string; randstr?: string}
type CaptchaInstance = {show: () => void; destroy: () => void}
declare global {
  interface Window {
    TencentCaptcha?: new (appId: string, callback: (result: CaptchaResult) => void) => CaptchaInstance
  }
}
let loading: Promise<void> | undefined
function load(): Promise<void> {
  if (window.TencentCaptcha) return Promise.resolve()
  if (loading) return loading
  loading = new Promise<void>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = 'https://turing.captcha.qcloud.com/TJCaptcha.js'
    script.async = true
    const fail = () => {
      clearTimeout(timer); script.remove(); loading = undefined
      reject(new Error('人机验证加载失败，请检查网络后重试'))
    }
    const timer = window.setTimeout(fail, 15000)
    script.onerror = fail
    script.onload = () => {
      if (!window.TencentCaptcha) { fail(); return }
      clearTimeout(timer); resolve()
    }
    document.head.appendChild(script)
  })
  return loading
}

export async function verifyTencentCaptcha(appId: string, signal: AbortSignal): Promise<string> {
  await load()
  if (signal.aborted || !window.TencentCaptcha) throw new Error('验证已取消，请重试')
  return new Promise((resolve, reject) => {
    let widget: CaptchaInstance | undefined
    let settled = false
    const finish = (proof?: string) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      signal.removeEventListener('abort', abort)
      widget?.destroy()
      if (proof) resolve(proof)
      else reject(new Error('人机验证未完成，请重试'))
    }
    const abort = () => finish()
    const timer = window.setTimeout(abort, 120000)
    signal.addEventListener('abort', abort, {once:true})
    try {
      widget = new window.TencentCaptcha!(appId, result => {
        const {ticket, randstr} = result
        if (result.ret !== 0 || !ticket || !randstr || ticket.startsWith('trerror_')) { finish(); return }
        const proof = JSON.stringify({ticket,randstr})
        finish(proof.length <= 2048 ? proof : undefined)
      })
      if (settled) widget.destroy()
      else widget.show()
    } catch { finish() }
  })
}
