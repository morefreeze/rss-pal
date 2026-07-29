import { useEffect } from 'react'

// Measured against the production origin: an idle HTTPS connection survives a
// 30s gap reliably, is already flaky at 60s, and is always dead by 90s — an
// intermediate NAT drops the flow well before nginx's own keepalive_timeout.
// Re-establishing it costs a ~3s cross-border TLS handshake, while a request on
// a warm connection lands in ~0.4s. Pinging every 30s keeps the flow alive.
const DEFAULT_INTERVAL_MS = 30_000

// Browsers pool a single HTTP/2 connection per origin, so this 34-byte GET
// keeps alive the exact connection the real API calls will reuse. /api/health
// is public and registered GET-only (HEAD returns 404), so GET it is.
const HEALTH_PATH = '/api/health'

interface NetworkInformationLike {
  saveData?: boolean
  effectiveType?: string
}

function shouldSkipForConnection(): boolean {
  const connection = (
    navigator as Navigator & { connection?: NetworkInformationLike }
  ).connection
  if (!connection) return false
  if (connection.saveData) return true
  return connection.effectiveType === 'slow-2g' || connection.effectiveType === '2g'
}

/**
 * Keeps the origin connection warm while the user browses, so opening an
 * article reuses the existing TLS session instead of paying a fresh handshake.
 *
 * Pauses entirely while the tab is hidden — waking the radio every 30s in the
 * background is a real battery cost for no benefit — and pings once immediately
 * on return, since the hidden period has almost certainly exceeded the limit.
 */
export function useConnectionKeepAlive(
  enabled: boolean,
  intervalMs: number = DEFAULT_INTERVAL_MS,
) {
  useEffect(() => {
    if (!enabled) return
    if (shouldSkipForConnection()) return

    let timerID: ReturnType<typeof setInterval> | null = null

    const ping = () => {
      if (document.hidden) return
      void fetch(HEALTH_PATH, { method: 'GET', cache: 'no-store' }).catch(() => {})
    }

    const start = () => {
      if (timerID !== null) return
      timerID = setInterval(ping, intervalMs)
    }

    const stop = () => {
      if (timerID === null) return
      clearInterval(timerID)
      timerID = null
    }

    const handleVisibilityChange = () => {
      if (document.hidden) {
        stop()
        return
      }
      ping()
      start()
    }

    if (!document.hidden) start()
    document.addEventListener('visibilitychange', handleVisibilityChange)

    return () => {
      stop()
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [enabled, intervalMs])
}
