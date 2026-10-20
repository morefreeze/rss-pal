import { useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { createOneoffLinkSet, isLoggedIn } from '../api/client'
import { safePublicSourceURL } from '../utils/authIntent'

function sharedURL(search: string): string | null {
  try {
    decodeURIComponent(search.replace(/\+/g, ' '))
    const urls = new URLSearchParams(search).getAll('url')
    return urls.length === 1 ? safePublicSourceURL(urls[0]) : null
  } catch { return null }
}

export default function ShareCapturePage() {
  const { search } = useLocation()
  const url = sharedURL(search)
  // A new incoming share gets its own state even during SPA navigation.
  return <CaptureForm key={url ?? search} url={url} />
}

function CaptureForm({ url }: { url: string | null }) {
  const inFlight = useRef(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [articleID, setArticleID] = useState<number | null>(null)

  const capture = async () => {
    if (!url || inFlight.current || articleID !== null) return
    inFlight.current = true
    setBusy(true)
    setError('')
    try {
      const result = await createOneoffLinkSet(url, false)
      setArticleID(result.parent_article_id)
    } catch {
      setError('抓取失败，请稍后重试。需要登录才能查看的网页可能无法直接抓取。')
    } finally {
      inFlight.current = false
      setBusy(false)
    }
  }

  return <main className="card" style={{ maxWidth: 560, margin: '32px auto', padding: 20 }}>
    <h1 style={{ fontSize: 22, marginBottom: 16 }}>加入 RSS Pal</h1>
    {!url ? <p role="alert">没有收到有效的网页链接，请从链接的“共享”菜单重试。</p> : <>
      <p style={{ overflowWrap: 'anywhere', marginBottom: 16 }}>{url}</p>
      <p className="text-muted text-sm mb-2">抓取后保存到“网摘”。</p>
      {articleID !== null ? <div role="status">
        <p className="mb-2">已保存到网摘</p>
        <Link to={`/articles/${articleID}`}>查看文章</Link>
      </div> : !isLoggedIn() ? <Link to={`/login?intent=capture&source=${encodeURIComponent(url)}`}>登录后继续</Link> : <>
        {error && <p role="alert" className="mb-2">{error}</p>}
        <button type="button" disabled={busy} onClick={() => void capture()} style={{ minHeight: 44 }}>
          {busy ? '正在抓取…' : error ? '重试抓取' : '加入抓取'}
        </button>
      </>}
    </>}
    <p style={{ marginTop: 24 }}><Link to="/articles?view=clip">返回网摘</Link></p>
  </main>
}
