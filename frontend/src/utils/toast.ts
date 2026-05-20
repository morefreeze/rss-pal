type ToastType = 'success' | 'error' | 'info'

interface ToastOptions {
  // When set, renders an action button next to the message and uses this
  // timeout (in ms). Clicking the action dismisses the toast.
  action?: { label: string; onClick: () => void }
  // Override the default duration (3s for success/info, 5s for error).
  durationMs?: number
}

function show(type: ToastType, msg: string, opts?: ToastOptions) {
  window.dispatchEvent(new CustomEvent('show-toast', { detail: { msg, type, ...opts } }))
}

export const toast = {
  success: (msg: string, opts?: ToastOptions) => show('success', msg, opts),
  error: (msg: string, opts?: ToastOptions) => show('error', msg, opts),
  info: (msg: string, opts?: ToastOptions) => show('info', msg, opts),
}
