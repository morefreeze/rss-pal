export class QQPopupError extends Error {}

const flowKey = 'rsspal:account-oauth-flow'

// Open during the click, before the async start request can consume user activation.
export function createQQOAuthPopup(onComplete: () => void) {
  const popup = window.open('about:blank', '_blank',
    'width=450,height=320,menubar=0,scrollbars=1,resizable=1,status=1,titlebar=0,toolbar=0,location=1')
  if (!popup) throw new QQPopupError('请允许弹出窗口后重试 QQ 登录')
  let channel: BroadcastChannel
  const id = `rsspal-qq-${crypto.randomUUID()}`
  try {
    // QQ never needs access to the original RSS Pal window.
    popup.opener = null
    channel = new BroadcastChannel(id)
  } catch {
    popup.close()
    throw new QQPopupError('当前浏览器不支持独立窗口登录，请更新浏览器后重试')
  }
  let disposed = false
  const dispose = () => {
    if (disposed) return
    disposed = true
    clearInterval(timer)
    channel.close()
    popup.close()
  }
  // An opener policy on a provider page may sever the WindowProxy. Keep the
  // same-origin completion channel alive until completion, unmount or timeout.
  const timer = setInterval(() => {
    if (Date.now() - openedAt > 600000) dispose()
  }, 1000)
  const openedAt = Date.now()
  channel.onmessage = event => {
    if (event.data !== 'complete' || disposed) return
    dispose()
    onComplete()
  }
  return {
    navigate(url: string) {
      if (popup.closed) throw new QQPopupError('QQ 登录窗口已关闭，请重试')
      const target = new URL(url)
      if (target.origin !== 'https://graph.qq.com' || target.pathname !== '/oauth2.0/authorize')
        throw new QQPopupError('QQ 登录跳转无效，请重试')
      const flow = JSON.parse(sessionStorage.getItem(flowKey) || 'null')
      if (!flow) throw new QQPopupError('QQ 登录状态已失效，请重试')
      // about:blank copied sessionStorage before startAccountOAuth wrote the flow.
      popup.sessionStorage.setItem(flowKey, JSON.stringify({...flow, popupChannel: id}))
      popup.location.replace(target.href)
    },
    dispose,
  }
}

export function completeQQOAuthPopup(id?: string) {
  if (!id?.startsWith('rsspal-qq-')) return
  try {
    const channel = new BroadcastChannel(id)
    channel.postMessage('complete')
    channel.close()
    window.close()
  } catch {
    // Authentication succeeded; keep the callback page's normal navigation as
    // a fallback if the browser no longer permits the completion channel.
  }
}
