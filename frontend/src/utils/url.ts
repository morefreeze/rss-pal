// Resolves a possibly-relative href against the article's URL so it can be
// matched against absolute candidate URLs from the backend. Returns the
// original href if either input is unparseable (those will never match a
// candidate and fail safely).
//
// Must mirror backend's `normaliseLinkSetURL`
// (backend/internal/rss/linkset_extract.go) so candidate URLs stored by the
// worker line up with hrefs in the rendered markdown — otherwise the inline
// ⬇ icon never shows. Specifically:
//   - lowercase host
//   - drop fragment
//   - strip tracking query params (utm_*, ref, mc_cid, mc_eid, fbclid, gclid)
//   - sort remaining query keys (Go's url.Values.Encode sorts by key)
//   - strip trailing slash on path (when path is longer than "/")

const UTM_PARAM_RE = /^(utm_[a-z]+|ref|mc_cid|mc_eid|fbclid|gclid)$/i

export function normalizeURL(href: string, base?: string): string {
  try {
    const u = new URL(href, base)
    u.hash = ''
    u.hostname = u.hostname.toLowerCase()
    // Filter trackers + sort remaining keys (ASCII order, matching Go).
    const kept = [...u.searchParams.entries()]
      .filter(([k]) => !UTM_PARAM_RE.test(k))
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    const next = new URLSearchParams()
    for (const [k, v] of kept) next.append(k, v)
    u.search = next.toString()
    if (u.pathname.length > 1 && u.pathname.endsWith('/')) {
      u.pathname = u.pathname.replace(/\/+$/, '')
    }
    return u.toString()
  } catch {
    return href
  }
}
