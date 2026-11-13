import { useEffect, useRef } from 'react'
import { useLocation } from 'react-router-dom'
import { isLoggedIn } from '../api/client'

const visitorKey = 'rsspal.traffic.visitor'
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
const publicPage = /^\/(?:login|register|capture|extension-config|share\/[^/]+)\/?$/i
const privatePage = /^\/(?:articles(?:\/\d+)?|explore(?:\/articles\/\d+)?|feeds(?:\/health)?|daily|weekly|clip|interests|stats|history|settings)\/?$/i

function landingSource(search: string): string {
  const params = new URLSearchParams(search)
  if (params.get('utm_source') || params.get('utm_campaign')) return 'campaign'
  if (!document.referrer) return 'direct'
  try {
    return new URL(document.referrer).origin === window.location.origin ? 'internal' : 'external'
  } catch {
    return 'direct'
  }
}

export default function SiteTraffic() {
  const { pathname, search } = useLocation()
  const lastPath = useRef('')
  const entrance = useRef(landingSource(search))
  const reported = useRef(false)

  useEffect(() => {
    if (lastPath.current === pathname) return
    lastPath.current = pathname
    if (!window.crypto?.randomUUID || navigator.doNotTrack === '1') return

    try {
      if (!publicPage.test(pathname) && !(privatePage.test(pathname) && isLoggedIn())) return
      let visitor = localStorage.getItem(visitorKey)
      if (!visitor || !uuid.test(visitor)) {
        visitor = window.crypto.randomUUID()
        localStorage.setItem(visitorKey, visitor)
      }
      const source = reported.current ? 'internal' : entrance.current
      reported.current = true
      void fetch('/api/traffic/page-view', {
        method: 'POST',
        credentials: 'omit',
        headers: { 'Content-Type': 'application/json' },
        referrerPolicy: 'no-referrer',
        keepalive: true,
        body: JSON.stringify({ event: window.crypto.randomUUID(), visitor, source }),
      }).catch(() => {})
    } catch {
      // Traffic collection must never interrupt navigation.
    }
  }, [pathname])

  return null
}
