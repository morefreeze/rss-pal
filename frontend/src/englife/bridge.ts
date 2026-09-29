export function englifeBridge(type: 'PING' | 'AUTHORIZE', data?: { token: string; serverOrigin: string }): Promise<{ ok: boolean }> {
  return new Promise((resolve, reject) => {
    const requestId = crypto.randomUUID()
    const timer = window.setTimeout(() => { window.removeEventListener('message', receive); reject(new Error('EXTENSION_UNAVAILABLE')) }, 5000)
    function receive(event: MessageEvent) {
      if (event.source !== window || event.origin !== location.origin || event.data?.source !== 'rss-pal-englife-extension' || event.data.requestId !== requestId) return
      window.clearTimeout(timer)
      window.removeEventListener('message', receive)
      resolve({ ok: event.data.ok === true })
    }
    window.addEventListener('message', receive)
    window.postMessage({ source: 'rss-pal-englife-page', type, requestId, ...data }, location.origin)
  })
}
