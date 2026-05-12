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

const TOP_K_DISPLAY = 5

export function LinkSetChildren({ parentId, children, onChildrenUpdated }: Props) {
  const [items, setItems] = useState<Article[]>(children)
  const [pendingIds, setPendingIds] = useState<Set<number>>(new Set())

  useEffect(() => {
    setItems(children)
  }, [children])

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

  const topK = items.slice(0, TOP_K_DISPLAY)
  const rest = items.slice(TOP_K_DISPLAY)

  async function handleExpand(childId: number) {
    setPendingIds((p) => new Set(p).add(childId))
    try {
      await expandLinkSetChild(childId)
    } catch (e) {
      console.warn('expand failed', e)
      setPendingIds((p) => {
        const n = new Set(p)
        n.delete(childId)
        return n
      })
      return
    }
    const interval = setInterval(async () => {
      try {
        const data = await getArticle(parentId)
        const updatedChild = data.children?.find((c) => c.id === childId)
        if (
          updatedChild &&
          (updatedChild.processing_state === 'ready' || updatedChild.processing_state === 'failed')
        ) {
          clearInterval(interval)
          if (data.children) {
            setItems(data.children)
            onChildrenUpdated?.(data.children)
          }
          setPendingIds((p) => {
            const n = new Set(p)
            n.delete(childId)
            return n
          })
        }
      } catch (e) {
        console.warn('poll failed', e)
      }
    }, 4000)
    // Safety cap at 5 minutes
    setTimeout(() => clearInterval(interval), 5 * 60 * 1000)
  }

  return (
    <section className="mt-8 pt-6" style={{ borderTop: '1px solid var(--border)' }}>
      <h2 className="text-lg font-semibold mb-4">本期链接（{items.length} 条）</h2>

      {/* Top-K: full cards */}
      <div className="space-y-3">
        {topK.map((c) => (
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
            {c.processing_state === 'ready' && c.summary_brief && (
              <p className="text-sm mt-2" style={{ color: 'var(--fg)' }}>
                {c.summary_brief}
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
                onClick={() => handleExpand(c.id)}
              >
                重试
              </button>
            )}
          </article>
        ))}
      </div>

      {/* Rest: compact rows */}
      {rest.length > 0 && (
        <div className="mt-6">
          <h3 className="text-sm font-medium mb-2" style={{ color: 'var(--fg-muted)' }}>
            其余 {rest.length} 条
          </h3>
          <ul className="space-y-1">
            {rest.map((c) => (
              <li key={c.id} className="flex items-center gap-2 text-sm py-1">
                <Link
                  to={`/articles/${c.id}`}
                  className="flex-1 truncate hover:underline"
                  style={{ color: 'var(--fg)' }}
                >
                  {c.title}
                </Link>
                <span className="text-xs" style={{ color: 'var(--fg-muted)' }}>
                  {hostOf(c.url)}
                </span>
                {c.processing_state === 'stub' && !pendingIds.has(c.id) && (
                  <button
                    className="text-xs px-2 py-0.5 rounded"
                    style={{ border: '1px solid var(--border)', color: 'var(--fg)' }}
                    onClick={() => handleExpand(c.id)}
                  >
                    展开摘要
                  </button>
                )}
                {(pendingIds.has(c.id) || c.processing_state === 'processing') && (
                  <span className="text-xs" style={{ color: 'var(--fg-muted)' }}>
                    处理中…
                  </span>
                )}
                {c.processing_state === 'failed' && (
                  <button
                    className="text-xs px-2 py-0.5 rounded"
                    style={{ border: '1px solid var(--border)', color: 'var(--fg)' }}
                    onClick={() => handleExpand(c.id)}
                  >
                    重试
                  </button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  )
}
