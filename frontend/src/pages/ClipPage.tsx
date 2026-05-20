import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ArticleOrder,
  ArticleSort,
  GetClipParams,
  ClipItem,
  ClipListResponse,
  UserTag,
  getClip,
  listTags,
} from '../api/client'
import ArticleCard from '../components/ArticleCard'
import ClipTagSidebar, {
  ClipSelection,
  ClipSourceRow,
} from '../components/ClipTagSidebar'
import ClipTagChipBar from '../components/ClipTagChipBar'
import { usePlayer } from '../player/PlayerContext'
import { reportClick } from '../hooks/useExposureTracking'
import { useBreakpoint } from '../hooks/useBreakpoint'

const PAGE_SIZE = 20

const formatDate = (dateStr: string | null) => {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  return date.toLocaleDateString('zh-CN', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const stripMarkdown = (text: string) =>
  text
    .replace(/[#*`_~>\[\]]/g, '')
    .replace(/\n+/g, ' ')
    .replace(/•\s*/g, '')
    .replace(/▸\s*/g, '')
    .replace(/\s{2,}/g, ' ')
    .trim()

function selectionToParams(sel: ClipSelection): GetClipParams {
  switch (sel.kind) {
    case 'all':
      return {}
    case 'untagged':
      return { untagged: true }
    case 'tag':
      // Single-tag select. Backend still accepts `mode` and array form,
      // but we always send a single id so `mode` is irrelevant.
      return { tag_ids: [sel.id] }
    case 'source':
      return { source: sel.key }
  }
}

interface ClipPageProps {
  // When set, every /api/clip request is force-scoped to this feed via
  // `source=feed:<id>`. Used when ClipPage is embedded inside
  // ArticleListPage as the 网摘 (clipping) feed view, where the parent
  // feed dropdown already constrains the source.
  restrictToFeedId?: number
  // Path to record as the entry path on session storage when opening an
  // article — defaults to '/clip' (standalone page) but the embed passes
  // '/articles' so back-navigation lands on the article list.
  entryPath?: string
  // When undefined (standalone /clip route), sidebar is always shown.
  // When provided by ArticleListPage in clipping mode, follows the parent toggle.
  sidebarOpen?: boolean
  // Sort controls. Optional — when omitted, the server picks a default.
  // The /articles embed forwards the user's sort UI selection here.
  sortField?: ArticleSort
  sortDir?: ArticleOrder
  // Checkbox filters mirrored from /articles. Optional; when undefined
  // the standalone /clip route runs without them.
  unreadOnly?: boolean
  savedOnly?: boolean
  // Surfaces the internal tag/source selection so the embedding page can
  // wire features like mark-all-read to the same filter the user sees.
  onSelectionChange?: (sel: ClipSelection) => void
}

export default function ClipPage({
  restrictToFeedId,
  entryPath = '/clip',
  sidebarOpen,
  sortField,
  sortDir,
  unreadOnly,
  savedOnly,
  onSelectionChange,
}: ClipPageProps = {}) {
  const navigate = useNavigate()
  const bp = useBreakpoint()
  const player = usePlayer()
  const [tags, setTags] = useState<UserTag[]>([])
  // Sources are aggregated client-side from clip items' effective_source.
  // We only refresh this aggregation when the user is viewing an unfiltered
  // ('all' or 'untagged') list — filtering by source/tag would otherwise
  // collapse the sidebar to one row and lose the navigation tree.
  const [sources, setSources] = useState<ClipSourceRow[]>([])
  const [selection, setSelection] = useState<ClipSelection>({ kind: 'all' })
  const [items, setItems] = useState<ClipItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [offset, setOffset] = useState(0)
  const [focusedIdx, setFocusedIdx] = useState(-1)

  // Initial sidebar tag list
  useEffect(() => {
    listTags().then(setTags).catch(() => setTags([]))
  }, [])

  const params = useMemo(() => {
    const base = selectionToParams(selection)
    // When embedded under a specific feed (网摘 mode), force the source
    // filter to that feed unless the user has explicitly clicked a more
    // granular source row (host:...). The parent dropdown owns the
    // feed-scope, so we never want to broaden beyond it.
    let out: GetClipParams = base
    if (restrictToFeedId != null && selection.kind !== 'source') {
      out = { ...base, source: `feed:${restrictToFeedId}` }
    }
    if (sortField) out = { ...out, sort: sortField }
    if (sortDir) out = { ...out, order: sortDir }
    if (unreadOnly) out = { ...out, unread: true }
    if (savedOnly) out = { ...out, saved: true }
    return out
  }, [selection, restrictToFeedId, sortField, sortDir, unreadOnly, savedOnly])
  const paramsKey = JSON.stringify(params)

  const loadPage = useCallback(
    async (off: number, reset: boolean) => {
      if (reset) setLoading(true)
      else setLoadingMore(true)
      try {
        const resp: ClipListResponse = await getClip({
          ...params,
          limit: PAGE_SIZE,
          offset: off,
        })
        const data = resp.items || []
        if (reset) {
          setItems(data)
          setFocusedIdx(-1)
        } else {
          setItems(prev => [...prev, ...data])
        }
        setTotal(resp.total || 0)
        setOffset(off)
      } finally {
        setLoading(false)
        setLoadingMore(false)
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [paramsKey],
  )

  useEffect(() => {
    loadPage(0, true)
  }, [loadPage])

  // Refresh the source list whenever we look at an unfiltered set, so the
  // sidebar reflects the user's full clip-source taxonomy. We also load
  // a wider page (100) here because the page-size of 20 would otherwise
  // hide many distinct sources behind pagination.
  useEffect(() => {
    if (selection.kind !== 'all' && selection.kind !== 'untagged') return
    let cancelled = false
    getClip({
      ...(selection.kind === 'untagged' ? { untagged: true } : {}),
      // In 网摘 embed mode, scope the aggregation to the same feed so the
      // sidebar lists only sources actually present under this feed.
      ...(restrictToFeedId != null ? { source: `feed:${restrictToFeedId}` } : {}),
      limit: 100,
      offset: 0,
    })
      .then(resp => {
        if (cancelled) return
        const counts = new Map<string, ClipSourceRow>()
        for (const it of resp.items || []) {
          const es = it.effective_source
          if (!es?.key) continue
          const cur = counts.get(es.key)
          if (cur) cur.count += 1
          else counts.set(es.key, { key: es.key, title: es.title, count: 1 })
        }
        setSources(
          [...counts.values()].sort((a, b) => b.count - a.count || a.title.localeCompare(b.title)),
        )
      })
      .catch(() => {
        if (!cancelled) setSources([])
      })
    return () => {
      cancelled = true
    }
  }, [selection.kind, restrictToFeedId])

  const hasMore = items.length < total

  const loadMore = () => {
    if (loadingMore || !hasMore) return
    loadPage(offset + PAGE_SIZE, false)
  }

  const openArticle = (id: number) => {
    reportClick(id)
    try {
      const ids = items.map(a => a.id)
      const i = ids.indexOf(id)
      const start = Math.max(0, i - 50)
      const end = Math.min(ids.length, i + 51)
      sessionStorage.setItem('articleNavList', JSON.stringify(ids.slice(start, end)))
      sessionStorage.setItem('articleListScroll', String(window.scrollY))
      sessionStorage.setItem('articleEntryPath', entryPath)
    } catch {
      // ignore storage failures
    }
    navigate(`/articles/${id}`, { state: { from: entryPath } })
  }

  const handleSelect = (sel: ClipSelection) => {
    setSelection(sel)
  }

  useEffect(() => {
    onSelectionChange?.(selection)
  }, [selection, onSelectionChange])

  const headerLabel = (() => {
    switch (selection.kind) {
      case 'all':
        return '所有 Tag'
      case 'untagged':
        return '未打 tag 的收藏'
      case 'tag': {
        const name = tags.find(t => t.id === selection.id)?.name
        return name ? `Tag: ${name}` : '收藏'
      }
      case 'source':
        return `来源: ${selection.title}`
    }
  })()

  const renderList = () => {
    if (loading) return <div className="card">Loading...</div>
    if (items.length === 0) return <div className="card text-muted">暂无收藏文章</div>
    return (
      <>
        {items.map((it, idx) => (
          <ArticleCard
            key={it.id}
            article={it}
            manualTags={it.manual_tags}
            isRead={!!it.is_read}
            isFocused={focusedIdx === idx}
            idx={idx}
            onPlay={player.playArticle}
            formatDate={formatDate}
            stripMarkdown={stripMarkdown}
            onOpen={openArticle}
            onFocus={setFocusedIdx}
            sourceLabel={it.effective_source?.title}
          />
        ))}
        {hasMore && (
          <div style={{ textAlign: 'center', padding: 12 }}>
            <button
              className="secondary"
              onClick={loadMore}
              disabled={loadingMore}
              style={{ fontSize: 13, padding: '6px 16px' }}
            >
              {loadingMore ? '加载中...' : '加载更多'}
            </button>
          </div>
        )}
        {!hasMore && items.length > 0 && (
          <div style={{ textAlign: 'center', padding: 16, color: 'var(--fg-muted)', fontSize: 13 }}>
            — 已加载全部收藏 —
          </div>
        )}
      </>
    )
  }

  if (bp === 'phone') {
    return (
      <div>
        <ClipTagChipBar
          tags={tags}
          sources={sources}
          selection={selection}
          onSelect={handleSelect}
        />
        <div className="flex-between mb-2">
          <h2 style={{ margin: 0, fontSize: 18 }}>{headerLabel}</h2>
          <span className="text-muted text-sm">共 {total} 篇</span>
        </div>
        {renderList()}
      </div>
    )
  }

  return (
    <div
      style={{
        display: 'flex',
        gap: 0,
        alignItems: 'flex-start',
        minHeight: 'calc(100vh - 120px)',
      }}
    >
      {(sidebarOpen ?? true) && (
        <ClipTagSidebar
          tags={tags}
          sources={sources}
          selection={selection}
          onSelect={handleSelect}
        />
      )}
      <section style={{ flex: 1, minWidth: 0, paddingLeft: 16 }}>
        <div className="flex-between mb-2">
          <h2 style={{ margin: 0 }}>{headerLabel}</h2>
          <span className="text-muted text-sm">共 {total} 篇</span>
        </div>
        {renderList()}
      </section>
    </div>
  )
}
