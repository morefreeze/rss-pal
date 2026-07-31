type TauriWindow = Window & {
  __TAURI__?: unknown
  __TAURI_IPC__?: unknown
  ipc?: {
    postMessage?: unknown
  }
}

const PAKE_WEBVIEW_UA =
  /Macintosh; Intel Mac OS X 10_15_7.*AppleWebKit\/605\.1\.15.*Version\/16\.1 Safari\/605\.1\.15/

export function isPakeWebView(win: Window = window): boolean {
  const candidate = win as TauriWindow
  return Boolean(
    candidate.__TAURI__ ||
      candidate.__TAURI_IPC__ ||
      typeof candidate.ipc?.postMessage === 'function' ||
      PAKE_WEBVIEW_UA.test(win.navigator.userAgent),
  )
}
