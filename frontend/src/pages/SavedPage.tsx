import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Feed,
  GetSavedParams,
  SavedItem,
  SavedListResponse,
  UserTag,
  getFeeds,
  getSaved,
  listTags,
} from '../api/client'
import ArticleCard from '../components/ArticleCard'
import SavedTagSidebar, { SavedSelection } from '../components/SavedTagSidebar'
import { usePlayer } from '../player/PlayerContext'
import { reportClick } from '../hooks/useExposureTracking'

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

function selectionToParams(sel: SavedSelection): GetSavedParams {
  switch (sel.kind) {
    case 'all':
      return {}
    case 'untagged':
      return { untagged: true }
    case 'tag':
      return { tag_ids: [sel.id] }
    case 'source':
      return { source_feed_id: sel.feedId }
  }
}

export default function SavedPage() {
  const navigate = useNavigate()
  const player = usePlayer()
  const [tags, setTags] = useState<UserTag[]>([])
  const [feeds, setFeeds] = useState<Feed[]>([])
  const [selection, setSelection] = useState<SavedSelection>({ kind: 'all' })
  const [items, setItems] = useState<SavedItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [offset, setOffset] = useState(0)
  const [focusedIdx, setFocusedIdx] = useState(-1)

  // Initial sidebar data
  useEffect(() => {
    listTags().then(setTags).catch(() => setTags([]))
    getFeeds().then(setFeeds).catch(() => setFeeds([]))
  }, [])

  const params = useMemo(() => selectionToParams(selection), [selection])
  const paramsKey = JSON.stringify(params)

  const loadPage = useCallback(
    async (off: number, reset: boolean) => {
      if (reset) setLoading(true)
      else setLoadingMore(true)
      try {
        const resp: SavedListResponse = await getSaved({
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
      sessionStorage.setItem('articleEntryPath', '/saved')
    } catch {
      // ignore storage failures
    }
    navigate(`/articles/${id}`, { state: { from: '/saved' } })
  }

  const sourceFeedTitle = (feedId: number) => {
    const f = feeds.find(x => x.id === feedId)
    return f ? f.title || f.url : `订阅 #${feedId}`
  }

  const headerLabel = (() => {
    switch (selection.kind) {
      case 'all':
        return '全部收藏'
      case 'untagged':
        return '未打 tag 的收藏'
      case 'tag': {
        const name = tags.find(t => t.id === selection.id)?.name
        return name ? `Tag: ${name}` : '收藏'
      }
      case 'source':
        return `来源: ${sourceFeedTitle(selection.feedId)}`
    }
  })()

  return (
    <div
      style={{
        display: 'flex',
        gap: 0,
        alignItems: 'flex-start',
        minHeight: 'calc(100vh - 120px)',
      }}
    >
      <SavedTagSidebar tags={tags} feeds={feeds} selection={selection} onSelect={setSelection} />
      <section style={{ flex: 1, minWidth: 0, paddingLeft: 16 }}>
        <div className="flex-between mb-2">
          <h2 style={{ margin: 0 }}>{headerLabel}</h2>
          <span className="text-muted text-sm">共 {total} 篇</span>
        </div>
        {loading ? (
          <div className="card">Loading...</div>
        ) : items.length === 0 ? (
          <div className="card text-muted">暂无收藏文章</div>
        ) : (
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
              <div style={{ textAlign: 'center', padding: 16, color: '#ccc', fontSize: 13 }}>
                — 已加载全部收藏 —
              </div>
            )}
          </>
        )}
      </section>
    </div>
  )
}
