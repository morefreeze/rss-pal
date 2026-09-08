import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import axios from 'axios'
import PublicArticleReader, { type SharedArticleSnapshot } from '../components/PublicArticleReader'

type LoadError = 'unavailable' | 'retryable' | null

export default function SharePage() {
  const { token } = useParams<{ token: string }>()
  const [article, setArticle] = useState<SharedArticleSnapshot | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<LoadError>(null)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const previousTitle = document.title
    let meta = document.querySelector<HTMLMetaElement>('meta[name="referrer"]')
    const createdMeta = !meta
    const previousContent = meta?.getAttribute('content') ?? null
    if (!meta) {
      meta = document.createElement('meta')
      meta.name = 'referrer'
      document.head.append(meta)
    }
    meta.content = 'no-referrer'

    return () => {
      document.title = previousTitle
      if (createdMeta) {
        meta?.remove()
      } else if (previousContent === null) {
        meta?.removeAttribute('content')
      } else {
        meta?.setAttribute('content', previousContent)
      }
    }
  }, [])

  useEffect(() => {
    if (article) document.title = `${article.title} - RSS Pal`
  }, [article])

  useEffect(() => {
    let current = true
    setLoading(true)
    setArticle(null)
    setError(null)

    if (!token) {
      setLoading(false)
      setError('unavailable')
      return () => { current = false }
    }

    axios.get<SharedArticleSnapshot>(`/api/share/${encodeURIComponent(token)}`)
      .then(response => {
        if (current) setArticle(response.data)
      })
      .catch((cause: unknown) => {
        if (!current) return
        const status = (cause as { response?: { status?: number } })?.response?.status
        setError(status === 404 ? 'unavailable' : 'retryable')
      })
      .finally(() => {
        if (current) setLoading(false)
      })

    return () => { current = false }
  }, [attempt, token])

  if (loading) {
    return <div className="public-reader-state card" aria-live="polite">加载中...</div>
  }

  if (error === 'unavailable' || (!article && !error)) {
    return <div className="public-reader-state card">分享链接无效或已过期</div>
  }

  if (error === 'retryable' || !article) {
    return (
      <div className="public-reader-state card">
        <p>暂时无法加载分享文章</p>
        <button type="button" onClick={() => setAttempt(value => value + 1)}>重试</button>
      </div>
    )
  }

  return <PublicArticleReader article={article} />
}
