import React from 'react'
import { Article, UserTag } from '../api/client'
import ReadingMeta from './ReadingMeta'
import TagChip from './TagChip'
import { useExposureTracking, reportClick } from '../hooks/useExposureTracking'

// MediaIndicator shows a per-article badge for media articles. Audio
// articles get a clickable ▶ play button (starts inline playback); video
// articles get a non-interactive 🎬 marker (video must play inside the
// article page where the embed lives). Rendered as siblings rather than
// either/or so an article that ever has both kinds of media displays
// both icons.
function MediaIndicator({ article, onPlay }: { article: Article; onPlay: (a: Article) => void }) {
  if (!article.media_url) return null
  const t = article.media_type ?? ''
  const isVideo = t.startsWith('video/')
  const isAudio = t.startsWith('audio/')
  // Articles with media_url but no recognised type fall back to the
  // play-button shape (the original behaviour).
  const audioFallback = !isVideo && !isAudio

  return (
    <span style={{ display: 'inline-flex', gap: 4, marginRight: 8, flexShrink: 0 }}>
      {isVideo && (
        <span
          title="视频"
          aria-label="视频"
          style={{
            padding: '2px 8px',
            borderRadius: 999,
            border: '1px solid #cc3a3a',
            background: '#fff5f5',
            color: '#cc3a3a',
            fontSize: 12,
          }}
        >
          🎬
        </span>
      )}
      {(isAudio || audioFallback) && (
        <button
          type="button"
          aria-label="播放"
          title="音频 · 点击播放"
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            onPlay(article)
          }}
          style={{
            padding: '2px 8px',
            borderRadius: 999,
            border: '1px solid #0066cc',
            background: '#fff',
            color: '#0066cc',
            fontSize: 12,
            cursor: 'pointer',
          }}
        >
          ▶
        </button>
      )}
    </span>
  )
}

interface Props {
  article: Article
  manualTags?: UserTag[]
  isRead: boolean
  isFocused: boolean
  idx: number
  prefetchRef?: React.RefObject<HTMLDivElement>
  onPlay: (a: Article) => void
  formatDate: (d: string | null) => string
  stripMarkdown: (t: string) => string
  onOpen: (id: number) => void
  onFocus: (idx: number) => void
  showSourceTag?: boolean
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
}: Props) {
  const exposureRef = useExposureTracking(article.id)

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
        outline: isFocused ? '2px solid #0066cc' : 'none',
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
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#0066cc', flexShrink: 0, marginTop: 6 }} />
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
          {(showSourceTag && article.feed_title) || manualTags.length > 0 ? (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center', marginTop: 4 }}>
              {showSourceTag && article.feed_title && (
                <TagChip name={article.feed_title} variant="source" />
              )}
              {manualTags.map(t => (
                <TagChip key={t.id} name={t.name} variant="manual" />
              ))}
            </div>
          ) : null}
          <div className="flex-between mt-1">
            <div className="flex gap-2" style={{ alignItems: 'center' }}>
              <span className="text-muted text-sm">{formatDate(article.published_at)}</span>
              <ReadingMeta wordCount={article.word_count} readingMinutes={article.reading_minutes} />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
