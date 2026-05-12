import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Article, expandLinkSetChild, getArticle } from '../api/client'

interface Props {
  parentId: number
  // eslint-disable-next-line react/no-unused-prop-types
  children: Article[]
  onChildrenUpdated?: (children: Article[]) => void
}

function hostOf(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

export function LinkSetChildren({ parentId, children, onChildrenUpdated }: Props) {
  const [items, setItems] = useState<Article[]>(children)

  useEffect(() => {
    // Sort by id ASC (creation order = user's selection order)
    const sorted = [...children].sort((a, b) => a.id - b.id)
    setItems(sorted)
  }, [children])

  // Poll for any processing children until they finish
  useEffect(() => {
    const processingIds = items.filter((c) => c.processing_state === 'processing').map((c) => c.id)
    if (processingIds.length === 0) return

    let cancelled = false
    const interval = setInterval(async () => {
      if (cancelled) return
      try {
        const data = await getArticle(parentId)
        if (cancelled) return
        if (data.children) {
          const sorted = [...data.children].sort((a, b) => a.id - b.id)
          setItems(sorted)
          onChildrenUpdated?.(data.children)
          // Stop polling once none are processing
          const stillProcessing = sorted.some((c) => c.processing_state === 'processing')
          if (!stillProcessing) {
            clearInterval(interval)
          }
        }
      } catch (e) {
        console.warn('poll failed', e)
      }
    }, 4000)

    // Safety cap at 5 minutes
    const safety = setTimeout(() => {
      cancelled = true
      clearInterval(interval)
    }, 5 * 60 * 1000)

    return () => {
      cancelled = true
      clearInterval(interval)
      clearTimeout(safety)
    }
  }, [items.map((c) => c.id + ':' + c.processing_state).join(',')])

  async function handleRetry(childId: number) {
    try {
      await expandLinkSetChild(childId)
    } catch (e) {
      console.warn('retry failed', e)
    }
  }

  if (items.length === 0) {
    return (
      <section
        className="mt-8 p-4 rounded text-sm"
        style={{ border: '1px solid var(--border)', color: 'var(--fg-muted)' }}
      >
        本期没有提取到链接。
      </section>
    )
  }

  return (
    <section className="mt-8 pt-6" style={{ borderTop: '1px solid var(--border)' }}>
      <h2 className="text-lg font-semibold mb-4">已抓取的链接（{items.length} 条）</h2>

      <div className="space-y-3">
        {items.map((c) => (
          <article
            key={c.id}
            className="p-4 rounded-md"
            style={{ border: '1px solid var(--border)', background: 'var(--bg-elevated)' }}
          >
            <Link
              to={`/articles/${c.id}`}
              className="font-medium hover:underline"
              style={{ color: 'var(--fg)' }}
            >
              {c.title}
            </Link>
            <div className="text-xs mt-1" style={{ color: 'var(--fg-muted)' }}>
              {hostOf(c.url)}
            </div>
            {c.editor_note && (
              <p className="text-sm mt-2 italic" style={{ color: 'var(--fg-muted)' }}>
                {c.editor_note}
              </p>
            )}
            {c.processing_state === 'processing' && (
              <p className="text-xs mt-2" style={{ color: 'var(--fg-muted)' }}>
                正在抓取并生成摘要…
              </p>
            )}
            {c.processing_state === 'failed' && (
              <button
                className="mt-2 text-xs px-2 py-1 rounded"
                style={{ border: '1px solid var(--border)', color: 'var(--fg)' }}
                onClick={() => handleRetry(c.id)}
              >
                重试
              </button>
            )}
            {c.processing_state === 'ready' && c.summary_brief && (
              <p className="text-sm mt-2" style={{ color: 'var(--fg)' }}>
                {c.summary_brief}
              </p>
            )}
          </article>
        ))}
      </div>
    </section>
  )
}
