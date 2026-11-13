export function isLennyPodcastEnclosure(rawURL: string, sourceURL?: string | null): boolean {
  if (!sourceURL) return false
  try {
    const source = new URL(sourceURL)
    const media = new URL(rawURL)
    return source.origin === 'https://www.lennysnewsletter.com'
      && source.pathname.startsWith('/p/')
      && ['https://api.substack.com', source.origin].includes(media.origin)
      && !source.username && !source.password && !media.username && !media.password
      && /^\/feed\/podcast\/[0-9]+\/[a-f0-9]{32}\.mp3$/.test(media.pathname)
  } catch {
    return false
  }
}

export function playableMediaURL(rawURL: string, sourceURL?: string | null): string {
  if (/^https?:\/\//i.test(rawURL)) {
    let source = ''
    if (sourceURL && isLennyPodcastEnclosure(rawURL, sourceURL)) {
      const { origin, pathname } = new URL(sourceURL)
      source = `&source=${encodeURIComponent(origin + pathname)}`
    }
    return `/api/proxy/media?url=${encodeURIComponent(rawURL)}${source}`
  }
  return rawURL
}
