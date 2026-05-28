// Resolves a possibly-relative href against the article's URL so it can be
// matched against absolute candidate URLs from the backend. Returns the
// original href if either input is unparseable (those will never match a
// candidate and fail safely).
export function normalizeURL(href: string, base?: string): string {
  try {
    return new URL(href, base).toString()
  } catch {
    return href
  }
}
