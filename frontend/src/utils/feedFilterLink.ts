export function buildFeedFilterPath(feedId: number, search = ''): string {
  const normalizedSearch = search.startsWith('?') ? search.slice(1) : search
  const params = new URLSearchParams(normalizedSearch)
  params.set('feed_id', String(feedId))
  params.delete('view')
  const query = params.toString()
  return query ? `/articles?${query}` : '/articles'
}

export function extractSearchFromPath(path: string): string {
  const queryStart = path.indexOf('?')
  if (queryStart < 0) return ''
  const hashStart = path.indexOf('#', queryStart)
  return hashStart < 0 ? path.slice(queryStart) : path.slice(queryStart, hashStart)
}

export function rememberFeedFilter(feedId: number): void {
  try { sessionStorage.setItem('selectedFeed', JSON.stringify(feedId)) } catch {}
}
