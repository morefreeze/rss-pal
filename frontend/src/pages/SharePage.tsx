import { useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import axios from 'axios'
import PublicArticleReader, { type SharedArticleSnapshot } from '../components/PublicArticleReader'

type LoadError = 'unavailable' | 'retryable' | null

type HeadOwner = {
  id: symbol
  title: string
}

type ReferrerMetaSnapshot = {
  element: HTMLMetaElement
  content: string | null
}

const headOwners: HeadOwner[] = []
let originalDocumentTitle = ''
let originalReferrerMetas: ReferrerMetaSnapshot[] = []
let managedReferrerMeta: HTMLMetaElement | null = null

function acquireShareHead(): symbol {
  if (headOwners.length === 0) {
    originalDocumentTitle = document.title
    originalReferrerMetas = [...document.querySelectorAll<HTMLMetaElement>('meta[name="referrer"]')]
      .map(element => ({ element, content: element.getAttribute('content') }))
    if (originalReferrerMetas.length === 0) {
      managedReferrerMeta = document.createElement('meta')
      managedReferrerMeta.name = 'referrer'
      document.head.append(managedReferrerMeta)
    }
    for (const { element } of originalReferrerMetas) element.content = 'no-referrer'
    if (managedReferrerMeta) managedReferrerMeta.content = 'no-referrer'
  }

  const id = Symbol('share-head-owner')
  headOwners.push({ id, title: 'RSS Pal' })
  document.title = 'RSS Pal'
  return id
}

function setShareHeadTitle(id: symbol | null, title: string) {
  if (!id) return
  const owner = headOwners.find(candidate => candidate.id === id)
  if (!owner) return
  owner.title = title
  if (headOwners[headOwners.length - 1]?.id === id) document.title = title
}

function releaseShareHead(id: symbol) {
  const index = headOwners.findIndex(candidate => candidate.id === id)
  if (index < 0) return
  headOwners.splice(index, 1)
  const activeOwner = headOwners[headOwners.length - 1]
  if (activeOwner) {
    document.title = activeOwner.title
    return
  }

  document.title = originalDocumentTitle
  for (const { element, content } of originalReferrerMetas) {
    if (content === null) element.removeAttribute('content')
    else element.setAttribute('content', content)
  }
  managedReferrerMeta?.remove()
  originalReferrerMetas = []
  managedReferrerMeta = null
}

export default function SharePage() {
  const { token } = useParams<{ token: string }>()
  const [article, setArticle] = useState<SharedArticleSnapshot | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<LoadError>(null)
  const [attempt, setAttempt] = useState(0)
  const headOwnerRef = useRef<symbol | null>(null)

  useEffect(() => {
    const owner = acquireShareHead()
    headOwnerRef.current = owner
    return () => {
      if (headOwnerRef.current === owner) headOwnerRef.current = null
      releaseShareHead(owner)
    }
  }, [])

  useEffect(() => {
    let current = true
    const controller = new AbortController()
    setLoading(true)
    setArticle(null)
    setError(null)
    setShareHeadTitle(headOwnerRef.current, 'RSS Pal')

    if (!token) {
      setLoading(false)
      setError('unavailable')
      return () => {
        current = false
        controller.abort()
      }
    }

    axios.get<SharedArticleSnapshot>(`/api/share/${encodeURIComponent(token)}`, { signal: controller.signal })
      .then(response => {
        if (!current || controller.signal.aborted) return
        setArticle(response.data)
        setShareHeadTitle(headOwnerRef.current, `${response.data.title} - RSS Pal`)
      })
      .catch((cause: unknown) => {
        if (!current || controller.signal.aborted) return
        const status = (cause as { response?: { status?: number } })?.response?.status
        setError(status === 404 ? 'unavailable' : 'retryable')
        setShareHeadTitle(headOwnerRef.current, 'RSS Pal')
      })
      .finally(() => {
        if (current) setLoading(false)
      })

    return () => {
      current = false
      controller.abort()
    }
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
