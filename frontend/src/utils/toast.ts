export const toast = {
  success: (msg: string) =>
    window.dispatchEvent(new CustomEvent('show-toast', { detail: { msg, type: 'success' } })),
  error: (msg: string) =>
    window.dispatchEvent(new CustomEvent('show-toast', { detail: { msg, type: 'error' } })),
  info: (msg: string) =>
    window.dispatchEvent(new CustomEvent('show-toast', { detail: { msg, type: 'info' } })),
}
