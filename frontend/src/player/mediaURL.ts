export function playableMediaURL(rawURL: string): string {
  if (/^https?:\/\//i.test(rawURL)) {
    return `/api/proxy/media?url=${encodeURIComponent(rawURL)}`
  }
  return rawURL
}
