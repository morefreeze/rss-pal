import { useState, useEffect, useCallback, useRef } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { getArticles, getGroupedArticles, searchArticles, getRecommended, markAllRead, Article, ArticleSort, ArticleOrder, Feed, GroupedArticles, getFeeds, likeArticle, dislikeArticle, getTagSidebar, TagSidebarData } from '../api/client'
import { writeNav, type NavContext } from '../utils/articleNav'
import ReadingMeta from '../components/ReadingMeta'
import ArticleCard from '../components/ArticleCard'
import BackToTopButton from '../components/BackToTopButton'
import GroupedArticleView from '../components/GroupedArticleView'
import ClipPage from './ClipPage'
import type { ClipSelection } from '../components/ClipTagSidebar'
import TagSidebar, { TagFilter } from '../components/TagSidebar'
import SidebarToggleButton from '../components/SidebarToggleButton'
import OverflowMenu from '../components/OverflowMenu'
import { usePlayer } from '../player/PlayerContext'
import { useExposureTracking, reportClick } from '../hooks/useExposureTracking'
import { useBreakpoint } from '../hooks/useBreakpoint'

const PAGE_SIZE = 20

// PREFETCH_OFFSET attaches the IntersectionObserver to the Nth-from-last
// article rather than a sentinel below the list, so the next page starts
// fetching while the user still has ~7 articles left to read.
const PREFETCH_OFFSET = 7

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

// SearchArticleRow renders a single article card in the search-results panel.
function SearchArticleRow({
  article,
  isRead,
  isFocused,
  idx,
  onPlay,
  formatDate,
  stripMarkdown,
  onNavigate,
  onFocus,
  navList,
}: {
  article: Article
  isRead: boolean
  isFocused: boolean
  idx: number
  onPlay: (a: Article) => void
  formatDate: (d: string | null) => string
  stripMarkdown: (t: string) => string
  onNavigate: (id: number) => void
  onFocus: (idx: number) => void
  navList: number[]
}) {
  const exposureRef = useExposureTracking(article.id)

  return (
    <div
      ref={exposureRef}
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
        // Search results are one-shot — clear any paginated load-more context
        // so the article page doesn't try to fetch the next page of /articles
        // for a list that the user reached via search.
        writeNav(navList, null)
        onNavigate(article.id)
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
          <div className="flex-between mt-1">
            <div className="flex gap-2" style={{ alignItems: 'center' }}>
              <span className="text-muted text-sm">{formatDate(article.published_at)}</span>
              <ReadingMeta wordCount={article.word_count} readingMinutes={article.reading_minutes} />
            </div>
            {article.feed_title && (
              <span className="text-sm" style={{ padding: '1px 6px', background: 'var(--accent-soft)', borderRadius: 4, color: 'var(--accent)' }}>
                {article.feed_title}
              </span>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

export default function ArticleListPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const wantsClip = searchParams.get('view') === 'clip'
  const player = usePlayer()
  const breakpoint = useBreakpoint()
  // On phone the toolbar tucks non-priority controls (search, feed select,
  // sort, 分组) under a ⋯ menu so the row only carries 仅未读 / 已保存 / 全部已读.
  const compactToolbar = breakpoint === 'phone'
  const [articles, setArticles] = useState<Article[]>([])
  const [recommended, setRecommended] = useState<Article[]>([])
  const [boostedIds, setBoostedIds] = useState<Set<number>>(new Set())
  const [feeds, setFeeds] = useState<Feed[]>([])
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [selectedFeed, setSelectedFeed] = useState<number | null>(() => {
    try { return JSON.parse(sessionStorage.getItem('selectedFeed') || 'null') } catch { return null }
  })
  // ?saved=1 (set by the /saved legacy redirect) force-ticks the saved
  // checkbox at mount time so the very first fetch already carries the
  // filter — avoids a transient unfiltered render that would race the
  // refetch.
  const savedRedirect = (() => {
    try { return new URLSearchParams(window.location.search).get('saved') === '1' }
    catch { return false }
  })()
  const [unreadOnly, setUnreadOnly] = useState(() => {
    if (savedRedirect) return false
    try { return sessionStorage.getItem('unreadOnly') === 'true' } catch { return false }
  })
  const [savedOnly, setSavedOnly] = useState(() => {
    if (savedRedirect) return true
    try { return sessionStorage.getItem('savedOnly') === 'true' } catch { return false }
  })
  const [sortField, setSortField] = useState<ArticleSort>(() => {
    try {
      const v = sessionStorage.getItem('articlesSortField')
        ?? sessionStorage.getItem('articlesSort') // legacy fallback
      return v === 'captured' ? 'captured' : 'published'
    } catch { return 'published' }
  })
  const [sortDir, setSortDir] = useState<ArticleOrder>(() => {
    try { return sessionStorage.getItem('articlesSortDir') === 'asc' ? 'asc' : 'desc' } catch { return 'desc' }
  })
  const [showRecommended, setShowRecommended] = useState(() => {
    try { return localStorage.getItem('showRecommended') === 'true' } catch { return false }
  })
  const [grouped, setGrouped] = useState(() => {
    try { return sessionStorage.getItem('articlesGrouped') === 'true' } catch { return false }
  })
  const [groupedData, setGroupedData] = useState<GroupedArticles | null>(null)
  const [groupedLoading, setGroupedLoading] = useState(false)
  const [offset, setOffset] = useState(0)
  const [hasMore, setHasMore] = useState(true)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<Article[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [markingAllRead, setMarkingAllRead] = useState(false)
  const loadMoreRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const [sessionReadIds, setSessionReadIds] = useState<Set<number>>(() => {
    try {
      return new Set(JSON.parse(sessionStorage.getItem('readArticles') || '[]'))
    } catch { return new Set() }
  })
  const [sidebarOpen, setSidebarOpen] = useState<boolean>(() => {
    try { return localStorage.getItem('tagSidebarOpen') === 'true' } catch { return false }
  })
  const [tagFilter, setTagFilter] = useState<TagFilter>(() => {
    try {
      const raw = sessionStorage.getItem('articleTagFilter')
      return raw ? JSON.parse(raw) as TagFilter : { kind: 'all' }
    } catch { return { kind: 'all' } }
  })
  const [tagSidebarData, setTagSidebarData] = useState<TagSidebarData | null>(null)
  const [focusedIdx, setFocusedIdx] = useState<number>(-1)
  const searchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Mirrors the embedded ClipPage's tag/source selection so the toolbar
  // (mark-all-read in particular) acts on the same subset the user sees.
  const [clipSelection, setClipSelection] = useState<ClipSelection>({ kind: 'all' })
  // Bumped after mark-all-read in clip mode to force ClipPage to refetch.
  const [clipRefreshKey, setClipRefreshKey] = useState(0)

  // 网摘 (clip) mode: when the selected feed is the user's clip bin,
  // swap the regular article list for the tag-sidebar layout that used
  // to live at /saved. The dropdown stays so the user can switch back
  // to other feeds, but unread/saved checkboxes, search, and
  // mark-all-read are hidden because /api/clip doesn't support them.
  const selectedFeedObj = feeds.find(f => f.id === selectedFeed)
  const isClippingMode = selectedFeedObj?.feed_type === 'clip'

  const toggleSidebar = useCallback(() => {
    setSidebarOpen(o => {
      const next = !o
      try { localStorage.setItem('tagSidebarOpen', String(next)) } catch {}
      return next
    })
  }, [])

  const selectTag = (sel: TagFilter) => {
    if (sel.kind !== 'all' && grouped) setGrouped(false)
    setTagFilter(sel)
    try { sessionStorage.setItem('articleTagFilter', JSON.stringify(sel)) } catch {}
  }

  useEffect(() => {
    loadFeeds()
    loadRecommended()
    const onRefreshUnread = () => {
      try {
        setSessionReadIds(new Set(JSON.parse(sessionStorage.getItem('readArticles') || '[]')))
      } catch {}
    }
    window.addEventListener('refresh-unread', onRefreshUnread)
    return () => window.removeEventListener('refresh-unread', onRefreshUnread)
  }, [])

  // /saved (legacy bookmark) redirects here as /articles?saved=1. The
  // savedOnly state is already initialized to true by the lazy init above;
  // here we just persist that choice and strip the param so a reload
  // doesn't re-tick if the user clears the box.
  useEffect(() => {
    if (searchParams.get('saved') !== '1') return
    try {
      sessionStorage.setItem('savedOnly', 'true')
      sessionStorage.setItem('unreadOnly', 'false')
    } catch {}
    const next = new URLSearchParams(searchParams)
    next.delete('saved')
    setSearchParams(next, { replace: true })
  }, [searchParams, setSearchParams])

  // Reconcile the ?view=clip URL param with the dropdown selection.
  // - view=clip + non-clip selection → switch to the clip feed
  // - view=clip + no clip feed exists → leave selection null and show hint
  // - no view param + currently on clip feed → clear selection back to "all"
  useEffect(() => {
    if (feeds.length === 0) return
    const clipFeed = feeds.find(f => f.feed_type === 'clip')
    const selectedIsClip = feeds.find(f => f.id === selectedFeed)?.feed_type === 'clip'
    if (wantsClip) {
      if (clipFeed && selectedFeed !== clipFeed.id) {
        setSelectedFeed(clipFeed.id)
        try { sessionStorage.setItem('selectedFeed', JSON.stringify(clipFeed.id)) } catch {}
      } else if (!clipFeed && selectedFeed !== null) {
        setSelectedFeed(null)
        try { sessionStorage.setItem('selectedFeed', 'null') } catch {}
      }
    } else {
      if (selectedIsClip) {
        setSelectedFeed(null)
        try { sessionStorage.setItem('selectedFeed', 'null') } catch {}
      }
    }
  }, [feeds, wantsClip])

  useEffect(() => {
    // ClipPage component owns its own data fetching when in clipping
    // mode, so skip the regular /api/articles call to avoid a wasted
    // request and to keep the flicker out.
    if (isClippingMode) return
    if (grouped) {
      // Grouped mode fetches its own payload — single shot, no pagination.
      setGroupedLoading(true)
      getGroupedArticles({
        feed_id: selectedFeed || undefined,
        unread: unreadOnly || undefined,
        saved: savedOnly || undefined,
      })
        .then(setGroupedData)
        .finally(() => setGroupedLoading(false))
      return
    }
    setOffset(0)
    setHasMore(true)
    setFocusedIdx(-1)
    loadArticles(0, true)
  }, [selectedFeed, unreadOnly, savedOnly, isClippingMode, grouped, sortField, sortDir, tagFilter])

  useEffect(() => {
    if (!sidebarOpen || isClippingMode) return
    getTagSidebar({
      feed_id: selectedFeed || undefined,
      unread: unreadOnly || undefined,
      saved: savedOnly || undefined,
    }).then(setTagSidebarData).catch(() => setTagSidebarData(null))
  }, [sidebarOpen, isClippingMode, selectedFeed, unreadOnly, savedOnly])

  const loadFeeds = async () => {
    const data = await getFeeds()
    setFeeds(data || [])
  }

  const loadArticles = useCallback(async (off: number, reset: boolean) => {
    if (reset) setLoading(true)
    else setLoadingMore(true)

    try {
      const raw = await getArticles({
        feed_id: selectedFeed || undefined,
        unread: unreadOnly || undefined,
        saved: savedOnly || undefined,
        tag_id: tagFilter.kind === 'tag' ? tagFilter.id : undefined,
        untagged: tagFilter.kind === 'untagged' || undefined,
        limit: PAGE_SIZE,
        offset: off,
        sort: sortField,
        order: sortDir,
      })
      const data = raw || []
      if (reset) {
        setArticles(data)
        // Restore scroll position when navigating back from article page
        try {
          const savedScroll = sessionStorage.getItem('articleListScroll')
          if (savedScroll) {
            sessionStorage.removeItem('articleListScroll')
            setTimeout(() => window.scrollTo(0, Number(savedScroll)), 50)
          }
        } catch {}
      } else {
        setArticles(prev => [...prev, ...data])
      }
      setHasMore(data.length === PAGE_SIZE)
      setOffset(off)
    } finally {
      setLoading(false)
      setLoadingMore(false)
    }
  }, [selectedFeed, unreadOnly, savedOnly, sortField, sortDir, tagFilter])

  const loadMore = useCallback(() => {
    if (!loadingMore && hasMore) {
      loadArticles(offset + PAGE_SIZE, false)
    }
  }, [loadingMore, hasMore, offset, loadArticles])

  // Infinite scroll via IntersectionObserver.
  // articles.length is in the deps so the effect re-runs after the first
  // fetch mounts the prefetch-trigger card (without it, the effect runs once
  // on mount when loadMoreRef.current is still null and never re-attaches).
  // PREFETCH_OFFSET means the observer hops to a new card each time the list
  // grows, so we tear the old one down on every re-run.
  useEffect(() => {
    if (!loadMoreRef.current) return
    const observer = new IntersectionObserver(
      entries => { if (entries[0].isIntersecting) loadMore() },
      { rootMargin: '200px' }
    )
    observer.observe(loadMoreRef.current)
    return () => observer.disconnect()
  }, [loadMore, articles.length])

  const handleMarkAllRead = async () => {
    setMarkingAllRead(true)
    try {
      if (isClippingMode) {
        // 网摘 mode: forward the same tag/source filters the user sees so the
        // mark-read scope matches the visible list. The clip feed itself is
        // already implied via feedId, but we also pass it as the source when
        // no narrower source is selected — matches ClipPage.params.
        const sourceOverride =
          clipSelection.kind === 'source'
            ? clipSelection.key
            : selectedFeed != null
              ? `feed:${selectedFeed}`
              : undefined
        await markAllRead({
          feedId: selectedFeed,
          unread: unreadOnly,
          saved: savedOnly,
          tagIds: clipSelection.kind === 'tag' ? [clipSelection.id] : undefined,
          untagged: clipSelection.kind === 'untagged',
          source: sourceOverride,
        })
        setClipRefreshKey(k => k + 1)
      } else {
        await markAllRead({ feedId: selectedFeed, unread: unreadOnly, saved: savedOnly })
        // Mirror the per-article session tracking so the list still reflects the
        // change before the next fetch, without clobbering reads from other filters.
        const markedIds = articles.map(a => a.id)
        try {
          const stored: number[] = JSON.parse(sessionStorage.getItem('readArticles') || '[]')
          const merged = Array.from(new Set([...stored, ...markedIds]))
          sessionStorage.setItem('readArticles', JSON.stringify(merged))
          setSessionReadIds(new Set(merged))
        } catch {}
        if (unreadOnly) {
          setArticles([])
          setHasMore(false)
        } else {
          setArticles(prev => prev.map(a => ({ ...a, is_read: true })))
        }
      }
      window.dispatchEvent(new Event('refresh-unread'))
    } catch {
      // silent fail
    } finally {
      setMarkingAllRead(false)
    }
  }

  const loadRecommended = async () => {
    try {
      const data = await getRecommended(10)
      setRecommended(data || [])
    } catch {
      // Ignore errors for recommended
    }
  }

  const handleBoost = async (articleId: number, e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setBoostedIds(prev => {
      const next = new Set(prev)
      next.add(articleId)
      return next
    })
    try {
      await likeArticle(articleId)
    } catch {
      setBoostedIds(prev => {
        const next = new Set(prev)
        next.delete(articleId)
        return next
      })
    }
  }

  const handleDampen = async (articleId: number, e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setRecommended(prev => prev.filter(a => a.id !== articleId))
    try {
      await dislikeArticle(articleId)
      await loadRecommended()
    } catch {
      // Keep the row removed locally; next page load will resync.
    }
  }

  const handleSearchChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const q = e.target.value
    setSearchQuery(q)
    if (searchTimeout.current) clearTimeout(searchTimeout.current)
    if (!q.trim()) {
      setSearchResults(null)
      return
    }
    searchTimeout.current = setTimeout(async () => {
      setSearching(true)
      try {
        const results = await searchArticles(q.trim(), 30)
        setSearchResults(results || [])
      } catch {
        setSearchResults([])
      } finally {
        setSearching(false)
      }
    }, 400)
  }

  // Keyboard navigation: j/k moves focus, o/Enter opens focused article
  useEffect(() => {
    const displayedArticles = searchQuery
      ? (searchResults || [])
      : articles.filter(a => !unreadOnly || !sessionReadIds.has(a.id))

    const handler = (e: KeyboardEvent) => {
      if (['INPUT', 'TEXTAREA', 'SELECT'].includes((e.target as HTMLElement)?.tagName)) return
      if (e.key === '/') {
        e.preventDefault()
        searchRef.current?.focus()
        return
      }
      if (e.key === 't' || e.key === 'T') {
        e.preventDefault()
        toggleSidebar()
        return
      }
      if (e.key === 'j' || e.key === 'ArrowDown') {
        e.preventDefault()
        setFocusedIdx(i => {
          const next = Math.min(i + 1, displayedArticles.length - 1)
          // scroll into view after state update
          setTimeout(() => {
            const cards = document.querySelectorAll('[data-article-card]')
            cards[next]?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
          }, 0)
          return next
        })
      } else if (e.key === 'k' || e.key === 'ArrowUp') {
        e.preventDefault()
        setFocusedIdx(i => {
          const prev = Math.max(i - 1, 0)
          setTimeout(() => {
            const cards = document.querySelectorAll('[data-article-card]')
            cards[prev]?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
          }, 0)
          return prev
        })
      } else if ((e.key === 'o' || e.key === 'Enter') && focusedIdx >= 0) {
        const article = displayedArticles[focusedIdx]
        if (article) openArticle(article.id)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [articles, searchResults, searchQuery, unreadOnly, sessionReadIds, focusedIdx, toggleSidebar])

  const isRead = (article: Article) => article.is_read || sessionReadIds.has(article.id)

  const openArticle = (id: number) => {
    reportClick(id)
    try {
      const ids = articles.map(a => a.id)
      const i = ids.indexOf(id)
      const start = Math.max(0, i - 50)
      const end = Math.min(ids.length, i + 51)
      // Only attach a load-more context when the clicked article actually
      // belongs to the paginated list and there's more to fetch. Grouped /
      // search / unknown ids fall through with no context so the article page
      // simply disables Next at the end of the saved window.
      const context: NavContext | null = i >= 0 && hasMore
        ? {
            kind: 'articles',
            params: {
              feed_id: selectedFeed || undefined,
              unread: unreadOnly || undefined,
              saved: savedOnly || undefined,
              tag_id: tagFilter.kind === 'tag' ? tagFilter.id : undefined,
              untagged: tagFilter.kind === 'untagged' || undefined,
              sort: sortField,
              order: sortDir,
            },
            nextOffset: offset + PAGE_SIZE,
            pageSize: PAGE_SIZE,
          }
        : null
      writeNav(ids.slice(start, end), context)
      sessionStorage.setItem('articleListScroll', String(window.scrollY))
      sessionStorage.setItem('articleEntryPath', '/articles')
    } catch {}
    navigate(`/articles/${id}`, { state: { from: '/articles' } })
  }

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return ''
    const date = new Date(dateStr)
    return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  const stripMarkdown = (text: string) =>
    text
      .replace(/[#*`_~>\[\]]/g, '')
      .replace(/\n+/g, ' ')
      .replace(/•\s*/g, '')
      .replace(/▸\s*/g, '')
      .replace(/\s{2,}/g, ' ')
      .trim()

  return (
    <div style={{ display: 'flex', minHeight: '100vh' }}>
      {sidebarOpen && !isClippingMode && tagSidebarData && (
        <TagSidebar data={tagSidebarData} selection={tagFilter} onSelect={selectTag} />
      )}
      <div style={{ flex: 1, minWidth: 0 }}>
      <div className="flex-between mb-2">
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {!isClippingMode && <SidebarToggleButton open={sidebarOpen} onToggle={toggleSidebar} />}
          <h2 style={{ margin: 0 }}>{isClippingMode ? '网摘' : '文章列表'}</h2>
        </div>
        {(() => {
          const searchEl = !isClippingMode && (
            <input
              ref={searchRef}
              type="search"
              placeholder="搜索文章... ( / 聚焦)"
              value={searchQuery}
              onChange={handleSearchChange}
              className="toolbar-control"
              style={{ width: compactToolbar ? '100%' : 200 }}
            />
          )
          const feedSelectEl = (
            <select
              value={selectedFeed || ''}
              onChange={e => {
                const val = e.target.value ? Number(e.target.value) : null
                setSelectedFeed(val)
                try { sessionStorage.setItem('selectedFeed', JSON.stringify(val)) } catch {}
                const pickedClip = val != null && feeds.find(f => f.id === val)?.feed_type === 'clip'
                if (pickedClip && !wantsClip) {
                  setSearchParams({ view: 'clip' })
                } else if (!pickedClip && wantsClip) {
                  setSearchParams({})
                }
              }}
              className="toolbar-control"
              disabled={!!searchQuery}
              style={compactToolbar ? { width: '100%' } : undefined}
            >
              <option value="">全部订阅</option>
              {feeds.map(f => (
                <option key={f.id} value={f.id}>{f.title || f.url}{f.unread_count > 0 ? ` (${f.unread_count})` : ''}</option>
              ))}
            </select>
          )
          const sortEl = !searchQuery && !grouped && (() => {
            const pick = (field: ArticleSort) => {
              if (field === sortField) {
                const next: ArticleOrder = sortDir === 'desc' ? 'asc' : 'desc'
                setSortDir(next)
                try { sessionStorage.setItem('articlesSortDir', next) } catch {}
              } else {
                setSortField(field)
                try { sessionStorage.setItem('articlesSortField', field) } catch {}
              }
            }
            const arrow = sortDir === 'asc' ? '↑' : '↓'
            const btn = (field: ArticleSort, label: string) => {
              const active = sortField === field
              return (
                <button
                  type="button"
                  className={active ? '' : 'btn-ghost'}
                  onClick={() => pick(field)}
                  style={{ padding: '4px 8px', minWidth: 0 }}
                  title={
                    active
                      ? '再点切换升序/降序'
                      : `点击按${label}排序`
                  }
                >
                  {label}{active ? ` ${arrow}` : ''}
                </button>
              )
            }
            return (
              <div style={{ display: 'inline-flex', gap: 4 }}>
                {btn('published', '发布')}
                {btn('captured', '抓取')}
              </div>
            )
          })()
          const groupEl = !isClippingMode && !searchQuery && tagFilter.kind === 'all' && (
            <button
              type="button"
              className={grouped ? '' : 'btn-ghost'}
              onClick={() => {
                const next = !grouped
                setGrouped(next)
                try { sessionStorage.setItem('articlesGrouped', String(next)) } catch {}
              }}
              title={grouped ? '回到列表视图' : '按主题分组查看'}
            >
              📚 分组
            </button>
          )
          const unreadEl = (
            <label className="toolbar-checkbox">
              <input
                type="checkbox"
                checked={unreadOnly}
                onChange={e => {
                  setUnreadOnly(e.target.checked)
                  if (e.target.checked) setSavedOnly(false)
                  try { sessionStorage.setItem('unreadOnly', String(e.target.checked)) } catch {}
                }}
                disabled={!!searchQuery}
              />
              仅未读
            </label>
          )
          const savedEl = (
            <label className="toolbar-checkbox">
              <input
                type="checkbox"
                checked={savedOnly}
                onChange={e => {
                  setSavedOnly(e.target.checked)
                  if (e.target.checked) setUnreadOnly(false)
                  try { sessionStorage.setItem('savedOnly', String(e.target.checked)) } catch {}
                }}
                disabled={!!searchQuery}
              />
              已保存
            </label>
          )
          const markAllReadEl = !searchQuery && (isClippingMode || articles.length > 0) && (
            <button
              className="btn-ghost"
              onClick={handleMarkAllRead}
              disabled={markingAllRead}
            >
              {markingAllRead ? '处理中...' : '全部已读'}
            </button>
          )
          // On phone, the overflow only renders when there's at least one
          // hidden control. Tag-filtered / search-active states naturally
          // shed sort and 分组, so the menu can be skipped.
          const hasOverflow = !!(searchEl || feedSelectEl || sortEl || groupEl)
          return (
            <div className="flex gap-2" style={{ flexWrap: 'wrap' }}>
              {!compactToolbar && searchEl}
              {!compactToolbar && feedSelectEl}
              {unreadEl}
              {savedEl}
              {!compactToolbar && sortEl}
              {!compactToolbar && groupEl}
              {markAllReadEl}
              {compactToolbar && hasOverflow && (
                <OverflowMenu>
                  {searchEl}
                  {feedSelectEl}
                  {sortEl}
                  {groupEl}
                </OverflowMenu>
              )}
            </div>
          )
        })()}
      </div>

      {wantsClip && !isClippingMode && !feeds.find(f => f.feed_type === 'clip') && (
        <div className="text-muted" style={{ padding: 24, textAlign: 'center' }}>
          还没有网摘 — 安装浏览器扩展或书签后再来收藏文章。
        </div>
      )}

      {isClippingMode && selectedFeed != null && (
        <ClipPage
          key={clipRefreshKey}
          restrictToFeedId={selectedFeed}
          entryPath="/articles"
          sidebarOpen={true}
          sortField={sortField}
          sortDir={sortDir}
          unreadOnly={unreadOnly}
          savedOnly={savedOnly}
          onSelectionChange={setClipSelection}
        />
      )}

      {!isClippingMode && recommended.length > 0 && !searchQuery && !grouped && (
        <div className="rec-panel">
          <button
            type="button"
            className="rec-panel-header"
            aria-expanded={showRecommended}
            title={showRecommended ? '收起' : '展开'}
            onClick={() => {
              const next = !showRecommended
              setShowRecommended(next)
              try { localStorage.setItem('showRecommended', String(next)) } catch {}
            }}
          >
            <span aria-hidden className="rec-panel-arrow">
              {showRecommended ? '▼' : '▶'}
            </span>
            <h3 className="rec-panel-title">为你推荐</h3>
            <span className="text-muted text-sm" style={{ fontWeight: 'normal' }}>({recommended.length})</span>
          </button>
          {showRecommended && recommended.map(article => (
            <Link key={article.id} to={`/articles/${article.id}`} className="rec-row">
              <div className="flex-between">
                <div style={{ flex: 1 }}>
                  <div className="text-bold" style={{ display: 'flex', alignItems: 'center' }}>
                    <MediaIndicator article={article} onPlay={player.playArticle} />
                    <span>{article.title}</span>
                  </div>
                  <div className="flex gap-2 mt-1">
                    <span className="text-muted text-sm">{formatDate(article.published_at)}</span>
                    {article.feed_title && (
                      <span className="text-sm" style={{ padding: '1px 6px', background: 'var(--accent-soft)', borderRadius: 4, color: 'var(--accent)' }}>
                        {article.feed_title}
                      </span>
                    )}
                  </div>
                </div>
                <div className="rec-feedback">
                  {boostedIds.has(article.id) ? (
                    <span className="rec-feedback-badge">✓ 已多推</span>
                  ) : (
                    <>
                      <button
                        type="button"
                        className="rec-feedback-btn"
                        title="多推这类"
                        aria-label="多推这类"
                        onClick={(e) => handleBoost(article.id, e)}
                      >👍</button>
                      <button
                        type="button"
                        className="rec-feedback-btn"
                        title="少推这类"
                        aria-label="少推这类"
                        onClick={(e) => handleDampen(article.id, e)}
                      >👎</button>
                    </>
                  )}
                </div>
              </div>
            </Link>
          ))}
        </div>
      )}

      {/* Search results */}
      {!isClippingMode && searchQuery && (
        searching ? (
          <div className="card text-muted text-sm">搜索中...</div>
        ) : searchResults !== null ? (
          searchResults.length === 0 ? (
            <div className="card text-muted">未找到相关文章</div>
          ) : (
            <>
              <div className="text-muted text-sm mb-1" style={{ padding: '0 4px' }}>找到 {searchResults.length} 篇文章</div>
              {searchResults.map((article, idx) => (
                <SearchArticleRow
                  key={article.id}
                  article={article}
                  isRead={isRead(article)}
                  isFocused={focusedIdx === idx}
                  idx={idx}
                  onPlay={player.playArticle}
                  formatDate={formatDate}
                  stripMarkdown={stripMarkdown}
                  onNavigate={(id) => navigate(`/articles/${id}`)}
                  onFocus={setFocusedIdx}
                  navList={searchResults.map(a => a.id)}
                />
              ))}
            </>
          )
        ) : null
      )}

      {!isClippingMode && !searchQuery && grouped && tagFilter.kind === 'all' ? (
        groupedLoading ? (
          <div className="card">Loading...</div>
        ) : groupedData ? (
          <GroupedArticleView
            data={groupedData}
            isRead={isRead}
            formatDate={formatDate}
            stripMarkdown={stripMarkdown}
            onOpen={openArticle}
            onPlay={player.playArticle}
          />
        ) : (
          <div className="card text-muted">加载失败</div>
        )
      ) : !isClippingMode && !searchQuery && loading ? (
        <div className="card">Loading...</div>
      ) : !isClippingMode && !searchQuery && articles.length === 0 && feeds.length === 0 ? (
        <div className="card" style={{ textAlign: 'center', padding: '32px 16px' }}>
          <div style={{ fontSize: 40, marginBottom: 12 }}>📰</div>
          <div className="text-bold" style={{ marginBottom: 8, fontSize: 16 }}>还没有订阅</div>
          <div className="text-muted" style={{ marginBottom: 16 }}>去「订阅」页面添加你感兴趣的 RSS 源或网站，系统会自动抓取并生成 AI 摘要</div>
          <Link to="/feeds"><button>去添加订阅</button></Link>
        </div>
      ) : !isClippingMode && !searchQuery && articles.length === 0 ? (
        <div className="card text-muted">
          {unreadOnly ? '没有未读文章 🎉' : '暂无文章，订阅源正在抓取中...'}
        </div>
      ) : !isClippingMode && !searchQuery ? (() => {
        const filtered = articles.filter(a => !unreadOnly || !sessionReadIds.has(a.id))
        // Prefetch trigger sits PREFETCH_OFFSET items before the end so
        // the next page starts loading while the user still has reading
        // left. Falls back to position 0 when the list is shorter than
        // the offset, ensuring the observer attaches to *some* card and
        // the bottom-of-list visual indicator is purely informational.
        const prefetchIdx = hasMore && filtered.length > 0
          ? Math.max(0, filtered.length - PREFETCH_OFFSET)
          : -1
        return (
        <>
          {filtered.map((article, idx) => (
            <ArticleCard
              key={article.id}
              article={article}
              manualTags={article.manual_tags || []}
              isRead={isRead(article)}
              isFocused={focusedIdx === idx}
              idx={idx}
              prefetchRef={idx === prefetchIdx ? loadMoreRef : undefined}
              onPlay={player.playArticle}
              formatDate={formatDate}
              stripMarkdown={stripMarkdown}
              onOpen={openArticle}
              onFocus={setFocusedIdx}
              dateField={sortField === 'captured' ? 'captured' : 'published'}
            />
          ))}
          {hasMore ? (
            <div style={{ textAlign: 'center', padding: '12px' }}>
              {loadingMore ? (
                <span style={{ color: 'var(--fg-muted)', fontSize: 13 }}>加载中...</span>
              ) : (
                <button
                  type="button"
                  onClick={loadMore}
                  className="secondary"
                  style={{ fontSize: 13, padding: '6px 16px' }}
                >
                  加载更多
                </button>
              )}
            </div>
          ) : articles.length > 0 ? (
            <div style={{ textAlign: 'center', padding: '16px', color: 'var(--fg-muted)', fontSize: 13 }}>
              — 已加载全部文章 —
            </div>
          ) : null}
        </>
        )
      })() : null}
      </div>
      <BackToTopButton />
    </div>
  )
}
