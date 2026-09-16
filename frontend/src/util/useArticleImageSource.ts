import { useEffect, useState } from 'react'
import { api } from '../api/client'

const PRIVATE_ARTICLE_IMAGE_RE = /^\/api\/articles\/[0-9]+\/images\/[0-9]+\.(?:png|jpe?g)$/
const PUBLIC_SHARE_ASSET_RE = /^(?:\/api\/share\/(?:[A-Za-z0-9]{8}|v1_[0-9a-f]{32}_[A-Za-z0-9_-]{43})|\/api\/s\/[0-9A-Za-z]{12})\/assets\/[0-9]+\.(?:png|jpe?g)$/

// Private figures use the same authenticated/refreshing client as article text.
// Blob URLs are released when the image changes or its reader unmounts.
export function useArticleImageSource(src: string | undefined): string | undefined {
  const privateImage = !!src && PRIVATE_ARTICLE_IMAGE_RE.test(src)
  const [loaded, setLoaded] = useState<{ source: string; url: string } | null>(null)
  useEffect(() => {
    if (!privateImage || !src) return
    const controller = new AbortController()
    let objectURL: string | undefined
    // A new cache key bypasses figures cached by the former public immutable route.
    api.get<Blob>(`${src.slice('/api'.length)}?__private=1`, {
      responseType: 'blob', signal: controller.signal,
      headers: { 'Cache-Control': 'no-cache, no-store', Pragma: 'no-cache' },
    })
      .then(({ data }) => {
        if (controller.signal.aborted) return
        objectURL = URL.createObjectURL(data)
        setLoaded({ source: src, url: objectURL })
      })
      .catch(() => { /* Never fall back to an anonymous private resource request. */ })
    return () => {
      controller.abort()
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [src, privateImage])
  return privateImage
    ? loaded?.source === src ? loaded.url : undefined
    : src ? PUBLIC_SHARE_ASSET_RE.test(src) ? src : `/api/proxy/image?url=${encodeURIComponent(src)}` : undefined
}
