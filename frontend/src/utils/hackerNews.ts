export function hackerNewsItemURL(raw: string): string | null {
  try {
    const value = raw.trim()
    const url = new URL(/^https?:\/\//i.test(value) ? value : `https://${value}`)
    const id = url.searchParams.get('id')
    if (!['http:', 'https:'].includes(url.protocol) || url.host !== 'news.ycombinator.com' || url.username || url.password || url.pathname !== '/item' || url.searchParams.getAll('id').length !== 1 || !id || !/^\d+$/.test(id) || BigInt(id) <= 0n || BigInt(id) > 9223372036854775807n) return null
    return `https://news.ycombinator.com/item?id=${BigInt(id)}`
  } catch { return null }
}
