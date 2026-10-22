import { useMemo, useState } from 'react'
import type { PublicCatalogItem } from '../api/feedCatalog'
import { SOURCE_CATEGORIES } from '../data/sourceDirectory'
import './SourceDirectory.css'

const LEGACY_CATEGORIES: Record<string, string> = {
  科技: '新媒体', 新闻: '传统媒体', 视频: '音视频',
}

interface Props {
  items: PublicCatalogItem[]
  onPreview: (url: string) => void
}

export default function CuratedSourceDirectory({ items, onPreview }: Props) {
  const [selected, setSelected] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const sources = useMemo(() => {
    const seen = new Set<string>()
    return items.filter(item => {
      const url = item.url.trim()
      if (seen.has(url)) return false
      seen.add(url)
      return true
    })
  }, [items])
  const categories = useMemo(() => {
    const base: { id: string; name: string; emoji: string; description: string }[] = [...SOURCE_CATEGORIES]
    const known = new Set(base.map(category => category.name))
    for (const item of sources) {
      const name = LEGACY_CATEGORIES[item.category] || item.category || '未分类'
      if (!known.has(name)) {
        known.add(name)
        base.push({ id: `custom:${name}`, name, emoji: '📡', description: '更多推荐源' })
      }
    }
    return base.map(category => ({
      ...category,
      items: category.id === 'popular' ? sources : sources.filter(item => (LEGACY_CATEGORIES[item.category] || item.category || '未分类') === category.name),
    }))
  }, [sources])
  const active = categories.find(category => category.id === selected)
  const term = query.trim().toLocaleLowerCase()
  const matches = term ? sources.filter(item => `${item.title} ${item.description} ${item.category} ${LEGACY_CATEGORIES[item.category] || ''}`.toLocaleLowerCase().includes(term)) : null
  const visible = matches ?? active?.items ?? []

  return (
    <section className="source-directory" aria-label="推荐源分类目录">
      <div className="source-directory-search">
        <input type="search" aria-label="搜索推荐源" placeholder="搜索全部推荐源…" value={query} onChange={event => setQuery(event.target.value)} />
        {query && <button type="button" className="secondary" onClick={() => setQuery('')}>清除搜索</button>}
      </div>
      {matches || active ? (
        <>
          <div className="source-directory-heading">
            <button type="button" className="secondary" onClick={() => { setSelected(null); setQuery('') }}>返回分类目录</button>
            <h3>{matches ? '搜索结果' : active?.name}</h3>
            <span className="text-muted" role="status">{visible.length} 个源</span>
          </div>
          <div className="source-directory-sources">
            {visible.map(source => (
              <article className="source-directory-source" key={source.url}>
                <div className="source-directory-source-copy">
                  <h4>{source.title}</h4>
                  <p className="text-muted text-sm">{source.description || source.category}</p>
                  <span className="text-muted text-sm">{new URL(source.url).hostname === 'rsshub.app' ? 'RSSHub' : new URL(source.url).hostname}</span>
                </div>
                <button type="button" className="secondary" aria-label={`预览 ${source.title}`} onClick={() => onPreview(source.url)}>预览</button>
              </article>
            ))}
          </div>
          {visible.length === 0 && <p className="text-muted" role="status">{matches ? '没有找到推荐源，试试其他关键词。' : '这个分类暂无已上架的推荐源。'}</p>}
        </>
      ) : (
        <div className="source-directory-grid">
          {categories.map(category => (
            <button type="button" className="source-directory-category" key={category.id} aria-label={`${category.name} ${category.items.length} 个源`} onClick={() => setSelected(category.id)}>
              <span aria-hidden="true" className="source-directory-emoji">{category.emoji}</span>
              <span className="source-directory-category-name">{category.name}<span className="text-muted text-sm">{category.items.length} 个源</span></span>
              <span className="text-muted text-sm source-directory-description">{category.description}</span>
            </button>
          ))}
        </div>
      )}
    </section>
  )
}
