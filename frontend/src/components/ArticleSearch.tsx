import { createContext, useContext, useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react'
import { createSearchHighlightPlugin } from '../utils/searchHighlight'
import { findSearchMatches } from '../utils/searchMatches'

const SearchContext = createContext('')
export function useArticleSearchQuery() { return useContext(SearchContext) }
export function ExcludeArticleSearch({ children }: { children: ReactNode }) {
  return <SearchContext.Provider value="">{children}</SearchContext.Provider>
}
export function useSearchHighlightPlugin() {
  const query = useContext(SearchContext)
  const id = useId()
  return useMemo(() => createSearchHighlightPlugin(query, id), [query, id])
}

export function SearchText({ text, query: suppliedQuery }: { text: string; query?: string }) {
  const contextualQuery = useContext(SearchContext)
  const query = suppliedQuery ?? contextualQuery
  const id = useId()
  const ranges = useMemo(() => findSearchMatches(text, query), [text, query])
  const nodes: ReactNode[] = []
  let cursor = 0
  ranges.forEach((range, index) => {
    nodes.push(text.slice(cursor, range.start))
    nodes.push(<mark key={index} className="search-match" data-search-match={`${id}-${index}`}>{text.slice(range.start, range.end)}</mark>)
    cursor = range.end
  })
  nodes.push(text.slice(cursor))
  return <>{nodes}</>
}

export function ArticleSearch({ query, articleKey, children, onClose }: { query: string; articleKey: string; children: ReactNode; onClose?: () => void }) {
  const [closedKey, setClosedKey] = useState('')
  const sessionKey = `${articleKey}:${query}`
  const activeQuery = closedKey === sessionKey ? '' : query.trim()
  const rootRef = useRef<HTMLDivElement>(null)
  const groupsRef = useRef<HTMLElement[][]>([])
  const indexRef = useRef(0)
  const [position, setPosition] = useState({ index: 0, total: 0 })
  const select = (index: number, scroll = true) => {
    const groups = groupsRef.current
    indexRef.current = groups.length ? (index + groups.length) % groups.length : 0
    groups.forEach((group, i) => group.forEach(mark => {
      if (i === indexRef.current) mark.setAttribute('data-search-current', 'true')
      else mark.removeAttribute('data-search-current')
    }))
    setPosition({ index: indexRef.current, total: groups.length })
    if (scroll) {
      const target = groups[indexRef.current]?.[0]
      for (let node = target?.parentElement; node; node = node.parentElement) {
        if (node instanceof HTMLDetailsElement) node.open = true
      }
      target?.scrollIntoView?.({ block: 'center', behavior: 'auto' })
    }
  }
  useEffect(() => {
    const root = rootRef.current
    if (!root || !activeQuery) return
    let previousIDs = ''
    let frame = 0
    indexRef.current = 0
    const scan = () => {
      const grouped = new Map<string, HTMLElement[]>()
      root.querySelectorAll<HTMLElement>('mark[data-search-match]').forEach(mark => {
        const id = mark.dataset.searchMatch!
        grouped.set(id, [...(grouped.get(id) || []), mark])
      })
      const signature = [...grouped.keys()].join('|')
      const changed = signature !== previousIDs || groupsRef.current[indexRef.current]?.[0] !== [...grouped.values()][indexRef.current]?.[0]
      previousIDs = signature
      groupsRef.current = [...grouped.values()]
      select(Math.min(indexRef.current, grouped.size - 1), changed)
    }
    const schedule = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(scan) }
    scan()
    const observer = new MutationObserver(schedule)
    observer.observe(root, { childList: true, subtree: true })
    return () => { observer.disconnect(); cancelAnimationFrame(frame); groupsRef.current = [] }
  }, [activeQuery, articleKey])
  return <SearchContext.Provider value={activeQuery}>
    <div ref={rootRef} className={activeQuery ? 'article-search-active' : undefined}>{children}</div>
    {activeQuery && <div className="article-search-toolbar" role="search" aria-label="文内搜索">
      <span className="article-search-query" title={activeQuery}>{activeQuery}</span>
      <output role="status" aria-live="polite">{position.total ? position.index + 1 : 0}/{position.total}</output>
      <button type="button" aria-label="上一个匹配" disabled={!position.total} onClick={() => select(indexRef.current - 1)}>‹</button>
      <button type="button" aria-label="下一个匹配" disabled={!position.total} onClick={() => select(indexRef.current + 1)}>›</button>
      <button type="button" aria-label="退出搜索" onClick={() => { setClosedKey(sessionKey); onClose?.() }}>×</button>
    </div>}
  </SearchContext.Provider>
}
