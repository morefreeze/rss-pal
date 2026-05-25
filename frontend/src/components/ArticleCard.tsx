import React from 'react'
import { Article, ArticleListItem, UserTag } from '../api/client'

// ArticleCardItem is what the card renders: anything with the lean
// list-item shape. Both the full Article (detail page) and the
// trimmed ArticleListItem (list endpoint) satisfy it, so the card is
// callable from either context.
type ArticleCardItem = Article | ArticleListItem
import ReadingMeta from './ReadingMeta'
import TagChip from './TagChip'
import { useExposureTracking, reportClick } from '../hooks/useExposureTracking'

// MediaIndicator shows a per-article badge for media articles. Audio
// articles get a clickable ▶ play button (starts inline playback); video
// articles get a non-interactive 🎬 marker (video must play inside the
// article page where the embed lives). Rendered as siblings rather than
// either/or so an article that ever has both kinds of media displays
// both icons.
function MediaIndicator({ article, onPlay }: { article: ArticleCardItem; onPlay: (a: ArticleCardItem) => void }) {
  if (!article.media_url) return null
  const t = article.media_type ?? ''
  const isVideo = t.startsWith('video/')
  const isAudio = t.startsWith('audio/')
  const audioFallback = !isVideo && !isAudio

  return (
    <span style={{ display: 'inline-flex', gap: 4, marginRight: 8, flexShrink: 0 }}>
      {isVideo && (
        <span
          title="视频"
          aria-label="视频"
          className="tag-chip"
          style={{ border: '1px solid var(--border)' }}
        >
          🎬 视频
        </span>
      )}
      {(isAudio || audioFallback) && (
        <button
          type="button"
          aria-label="播放"
          title="音频 · 点击播放"
          className="btn-ghost btn-sm"
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            onPlay(article)
          }}
          style={{ borderRadius: 999 }}
        >
          ▶ 音频
        </button>
      )}
    </span>
  )
}

interface Props {
  article: ArticleCardItem
  manualTags?: UserTag[]
  isRead: boolean
  isFocused: boolean
  idx: number
  prefetchRef?: React.RefObject<HTMLDivElement>
  onPlay: (a: ArticleCardItem) => void
  formatDate: (d: string | null) => string
  stripMarkdown: (t: string) => string
  onOpen: (id: number) => void
  onFocus: (idx: number) => void
  showSourceTag?: boolean
  // Override for the source chip text. /saved passes effective_source.title
  // here so bookmarklet articles show their real host instead of the
  // shared "⭐ 网摘" bin name.
  sourceLabel?: string
  // Which timestamp the card shows. Defaults to 'published'. When the list
  // is sorted by capture time, callers pass 'captured' so the user sees the
  // dimension the sort is driven by.
  dateField?: 'published' | 'captured'
}

// ArticleCard renders a single article row in the main list. It owns the
// exposure-tracking ref and reports a click event before navigation. The
// optional prefetchRef is merged onto the same element to keep the
// infinite-scroll observer working.
export default function ArticleCard({
  article,
  manualTags = [],
  isRead,
  isFocused,
  idx,
  prefetchRef,
  onPlay,
  formatDate,
  stripMarkdown,
  onOpen,
  onFocus,
  showSourceTag = true,
  sourceLabel,
  dateField = 'published',
}: Props) {
  const exposureRef = useExposureTracking(article.id)
  const effectiveSourceLabel = sourceLabel ?? article.feed_title

  // Merge the exposure ref with the optional prefetch (infinite-scroll) ref.
  const mergedRef = (el: HTMLDivElement | null) => {
    ;(exposureRef as React.MutableRefObject<HTMLDivElement | null>).current = el
    if (prefetchRef) {
      ;(prefetchRef as React.MutableRefObject<HTMLDivElement | null>).current = el
    }
  }

  return (
    <div
      ref={mergedRef}
      className="card"
      data-article-card
      style={{
        display: 'block',
        opacity: isRead ? 0.6 : 1,
        cursor: 'pointer',
        outline: isFocused ? '2px solid var(--accent)' : 'none',
        outlineOffset: -2,
      }}
      onClick={() => {
        onFocus(idx)
        reportClick(article.id)
        onOpen(article.id)
      }}
    >
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
        {!isRead && (
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--accent)', flexShrink: 0, marginTop: 6 }} />
        )}
        <div style={{ flex: 1 }}>
          <div className={isRead ? 'text-muted' : 'text-bold'} style={{ display: 'flex', alignItems: 'center' }}>
            <MediaIndicator article={article} onPlay={onPlay} />
            <span>{article.title}</span>
          </div>
          {article.summary_brief && (
            <div className="text-muted text-sm mt-1">
              {stripMarkdown(article.summary_brief).slice(0, 120)}...
            </div>
          )}
          {(showSourceTag && effectiveSourceLabel) || manualTags.length > 0 ? (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center', marginTop: 4 }}>
              {showSourceTag && effectiveSourceLabel && (
                <TagChip name={effectiveSourceLabel} variant="source" />
              )}
              {manualTags.map(t => (
                <TagChip key={t.id} name={t.name} variant="manual" />
              ))}
            </div>
          ) : null}
          <div className="flex-between mt-1">
            <div className="flex gap-2" style={{ alignItems: 'center' }}>
              <span className="text-muted text-sm">
                {dateField === 'captured' ? `抓 ${formatDate(article.fetched_at)}` : formatDate(article.published_at)}
              </span>
              <ReadingMeta wordCount={article.word_count} readingMinutes={article.reading_minutes} />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
