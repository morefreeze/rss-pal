import { useEffect, useRef, useState } from 'react'
import type { ExploreArticleListItem, ExploreSort } from '../api/client'

interface Props {
  article: ExploreArticleListItem
  sort: ExploreSort
  loadMoreRef?: React.RefObject<HTMLDivElement>
  onOpen: (article: ExploreArticleListItem) => void
  onExposure: (articleID: number) => Promise<boolean>
  onHideSource: (sourceID: number) => void
  onDampenTopic: (topic: string) => void
}

function formatDate(value: string | null) {
  if (!value) return '时间未知'
  return new Date(value).toLocaleDateString('zh-CN', {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  })
}

export default function ExploreArticleCard({
  article,
  sort,
  loadMoreRef,
  onOpen,
  onExposure,
  onHideSource,
  onDampenTopic,
}: Props) {
	const cardRef = useRef<HTMLElement | null>(null)
	const thumbnailSrc = article.thumbnail_url
		? article.thumbnail_url.startsWith('/api/articles/')
			? article.thumbnail_url
			: `/api/proxy/image?url=${encodeURIComponent(article.thumbnail_url)}`
		: undefined
  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const element = cardRef.current
    if (!element || typeof IntersectionObserver === 'undefined') return
    let timer: ReturnType<typeof setTimeout> | null = null
    let requestInFlight = false
    let stopped = false
    const observer = new IntersectionObserver(entries => {
      const visible = entries.some(entry => entry.isIntersecting && entry.intersectionRatio >= 0.5)
      if (visible && timer == null && !requestInFlight) {
        timer = setTimeout(() => {
          timer = null
          requestInFlight = true
          void onExposure(article.id)
            .then(recorded => {
              if (!stopped && recorded) observer.disconnect()
            })
            .catch(() => {
              // Keep observing so leaving and re-entering can retry.
            })
            .finally(() => {
              requestInFlight = false
            })
        }, 10_000)
      } else if (!visible && timer != null) {
        clearTimeout(timer)
        timer = null
      }
    }, { threshold: [0, 0.5, 1] })
    observer.observe(element)
    return () => {
      stopped = true
      if (timer != null) clearTimeout(timer)
      observer.disconnect()
    }
  }, [article.id, onExposure])

  useEffect(() => {
    if (!menuOpen) return
    const onPointerDown = (event: PointerEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) setMenuOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMenuOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [menuOpen])

  const assignRef = (element: HTMLElement | null) => {
    cardRef.current = element
    if (loadMoreRef) {
      ;(loadMoreRef as React.MutableRefObject<HTMLDivElement | null>).current = element as HTMLDivElement | null
    }
  }

  const open = () => onOpen(article)
  const shownDate = sort === 'captured' ? article.fetched_at : article.published_at

  return (
    <article
      ref={assignRef}
      role="article"
      aria-label={article.title}
      tabIndex={0}
      className="card explore-article-card"
      onClick={open}
      onKeyDown={event => {
        if (event.currentTarget !== event.target) return
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          open()
        }
      }}
    >
      <div className="explore-article-card__body">
        <div className="explore-article-card__content">
          <div className="explore-article-card__source-row">
            <span className="explore-source-chip">{article.source_title}</span>
            <span className="text-muted text-sm">
              {sort === 'captured' ? '抓取 ' : ''}{formatDate(shownDate)}
            </span>
            {article.is_subscribed && <span className="explore-subscribed-chip">已订阅</span>}
          </div>
          <h3>{article.title}</h3>
          {article.excerpt && <p className="explore-article-card__excerpt">{article.excerpt}</p>}
          <div className="explore-article-card__recommendation">
            <span>{article.topic || '综合'}</span>
            <span>{article.reason}</span>
          </div>
        </div>
        {thumbnailSrc && (
          <img
            className="explore-article-card__thumbnail"
            src={thumbnailSrc}
            alt={`${article.title} 缩略图`}
            loading="lazy"
            referrerPolicy="no-referrer"
          />
        )}
      </div>

      <div ref={menuRef} className="explore-card-menu">
        <button
          type="button"
          className="btn-ghost btn-sm"
          aria-label={`${article.title} 的更多操作`}
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          onClick={event => {
            event.stopPropagation()
            setMenuOpen(value => !value)
          }}
        >⋯</button>
        {menuOpen && (
          <div role="menu" className="explore-card-menu__popover" onClick={event => event.stopPropagation()}>
            <button type="button" role="menuitem" onClick={() => { setMenuOpen(false); onHideSource(article.source_id) }}>
              隐藏此源
            </button>
            <button type="button" role="menuitem" onClick={() => { setMenuOpen(false); onDampenTopic(article.topic) }}>
              少推荐这类内容
            </button>
          </div>
        )}
      </div>
    </article>
  )
}
