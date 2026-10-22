import { useMemo, useState } from 'react'
import type { PublicCatalogItem } from '../api/feedCatalog'
import type { DirectoryPlatform, DirectoryRoute, FullSourceDirectory } from '../api/sourceDirectory'
import { SOURCE_CATEGORIES } from '../data/sourceDirectory'
import CuratedSourceDirectory from './CuratedSourceDirectory'
import DirectoryRouteForm from './DirectoryRouteForm'
import './SourceDirectory.css'

interface Props {
  items: PublicCatalogItem[]
  directory?: FullSourceDirectory
  directoryLoading?: boolean
  directoryError?: boolean
  onReloadDirectory?: () => void
  onPreview: (url: string) => void
}
const CATEGORY_ALIASES: Record<string, string> = { 科技: '新媒体', 新闻: '传统媒体', 视频: '音视频' }
const PAGE_SIZE = 30

export default function SourceDirectory(props: Props) {
  return <>
    {props.directoryLoading && <p role="status">正在加载完整平台与订阅项目录…</p>}
    {props.directoryError && <div role="alert">完整目录加载失败 <button type="button" className="secondary" onClick={props.onReloadDirectory}>重试完整目录</button></div>}
    {props.directory
      ? <FullDirectoryBrowser items={props.items} directory={props.directory} onPreview={props.onPreview} />
      : !props.directoryLoading && <CuratedSourceDirectory items={props.items} onPreview={props.onPreview} />}
  </>
}

function FullDirectoryBrowser({ items, directory, onPreview }: Required<Pick<Props, 'items' | 'onPreview'>> & { directory: FullSourceDirectory }) {
  const [categoryID, setCategoryID] = useState<string | null>(null)
  const [platformID, setPlatformID] = useState<string | null>(null)
  const [routeID, setRouteID] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [limit, setLimit] = useState(PAGE_SIZE)
  const [routeLimit, setRouteLimit] = useState(PAGE_SIZE)
  const term = query.trim().toLocaleLowerCase()
  const categories = useMemo(() => {
    const known = new Set(SOURCE_CATEGORIES.map(category => category.name as string))
    const custom = [...new Set(items.map(item => item.category || '未分类'))].filter(name => !known.has(name) && !CATEGORY_ALIASES[name])
    return [
      { id: 'all-directory', name: '全部目录', emoji: '🌐', description: '浏览所有平台和订阅项' },
      ...SOURCE_CATEGORIES,
      ...custom.map(name => ({ id: `custom:${name}`, name, emoji: '📡', description: '管理员精选推荐' })),
    ].map(category => {
      const platforms = directory.platforms.map(platform => ({
        ...platform,
        routes: platform.routes.filter(route => category.id === 'all-directory' || (route.categories.length ? route.categories : platform.categories).includes(category.id)),
      })).filter(platform => platform.routes.length > 0)
      const sources = items.filter(item => category.id === 'popular' || (CATEGORY_ALIASES[item.category] || item.category || '未分类') === category.name)
      return { ...category, platforms, sources, routeCount: platforms.reduce((count, platform) => count + platform.routes.length, 0) }
    })
  }, [directory.platforms, items])
  const category = categories.find(item => item.id === categoryID)
  const platformResults = useMemo(() => {
    if (!term) return category?.platforms || []
    return directory.platforms.map(platform => {
      const platformMatch = `${platform.name} ${platform.id}`.toLocaleLowerCase().includes(term)
      return { ...platform, routes: platform.routes.filter(route => platformMatch || `${route.name} ${route.path} ${(route.paths || []).join(' ')} ${route.description}`.toLocaleLowerCase().includes(term)) }
    }).filter(platform => platform.routes.length > 0)
  }, [category, directory.platforms, term])
  const curated = useMemo(() => {
    const list = term ? items.filter(item => `${item.title} ${item.category} ${item.description}`.toLocaleLowerCase().includes(term)) : category?.sources || []
    const seen = new Set<string>()
    return list.filter(item => { if (seen.has(item.url)) return false; seen.add(item.url); return true })
  }, [category, items, term])
  const platform = platformResults.find(item => item.id === platformID)
  const route = platform?.routes.find(item => item.id === routeID)
  const routeCount = platformResults.reduce((count, item) => count + item.routes.length, 0)
  const resetNavigation = () => { setCategoryID(null); setPlatformID(null); setRouteID(null); setQuery(''); setLimit(PAGE_SIZE); setRouteLimit(PAGE_SIZE) }
  const search = (value: string) => { setQuery(value); setPlatformID(null); setRouteID(null); setLimit(PAGE_SIZE); setRouteLimit(PAGE_SIZE) }
  const openCategory = (id: string) => { setCategoryID(id); setPlatformID(null); setRouteID(null); setQuery(''); setLimit(PAGE_SIZE); setRouteLimit(PAGE_SIZE) }

  return <section className="source-directory" aria-label="完整订阅源目录">
    <p className="text-muted text-sm source-directory-metadata">
      全部上游目录：{directory.platform_count.toLocaleString()} 个平台 · {directory.route_count.toLocaleString()} 个订阅项 · 更新于 {new Date(directory.fetched_at).toLocaleDateString('zh-CN')}
      {directory.stale && '（当前使用缓存，稍后重试可获取更新）'}
      {' · '}<a href={directory.source_url} target="_blank" rel="noopener noreferrer">目录来源</a>
    </p>
    <p className="text-muted text-sm">目录收录不代表源站当前可用；需要账号或频道的订阅项，填写参数后预览。</p>
    <div className="source-directory-search">
      <input type="search" aria-label="搜索推荐源" placeholder="搜索全部平台、订阅项或精选源…" value={query} onChange={event => search(event.target.value)} />
      {query && <button type="button" className="secondary" onClick={() => search('')}>清除搜索</button>}
    </div>
    {route && platform ? <>
      <button type="button" className="secondary source-directory-back" onClick={resetNavigation}>返回分类目录</button>
      <DirectoryRouteForm key={`${platform.id}:${route.id}`} platform={platform} route={route} onPreview={onPreview} onBack={() => setRouteID(null)} />
    </> : category || term ? <>
      <div className="source-directory-heading">
        <button type="button" className="secondary" onClick={resetNavigation}>返回分类目录</button>
        {platform && <button type="button" className="secondary" onClick={() => { setPlatformID(null); setRouteLimit(PAGE_SIZE) }}>返回平台列表</button>}
        <h3>{platform?.name || (term ? '搜索结果' : category?.name)}</h3>
        <span className="text-muted" role="status">{platform ? `${platform.routes.length} 个订阅项` : `${platformResults.length} 个平台 · ${routeCount} 个订阅项`}</span>
      </div>
      {platform ? <RouteList platform={platform} routes={platform.routes.slice(0, routeLimit)} onConfigure={setRouteID} /> : <>
        {curated.length > 0 && <section aria-label="精选推荐">
          <h4>精选推荐 · {curated.length} 个源</h4>
          <div className="source-directory-sources">{curated.map(source => <article key={source.url} className="source-directory-source">
            <div className="source-directory-source-copy"><h4>{source.title}</h4><p className="text-muted text-sm">{source.description}</p><span className="text-muted text-sm">已上架精选</span></div>
            <button type="button" className="secondary" aria-label={`预览 ${source.title}`} onClick={() => onPreview(source.url)}>预览</button>
          </article>)}</div>
        </section>}
        <div className="source-directory-platforms">{platformResults.slice(0, limit).map(item => <article key={item.id} className="source-directory-platform">
          <div><h4>{item.name}</h4><span className="text-muted text-sm">{item.id} · {item.routes.length} 个订阅项</span><p className="text-muted text-sm">{item.routes.slice(0, 3).map(routeItem => routeItem.name).join(' · ')}</p></div>
          <button type="button" className="secondary" aria-label={`查看 ${item.name} 的 ${item.routes.length} 个订阅项`} onClick={() => { setPlatformID(item.id); setRouteLimit(PAGE_SIZE) }}>查看订阅项</button>
        </article>)}</div>
      </>}
      {!platform && limit < platformResults.length && <button type="button" className="secondary source-directory-more" onClick={() => setLimit(value => value + PAGE_SIZE)}>加载更多平台（已显示 {Math.min(limit, platformResults.length)}/{platformResults.length}）</button>}
      {platform && routeLimit < platform.routes.length && <button type="button" className="secondary source-directory-more" onClick={() => setRouteLimit(value => value + PAGE_SIZE)}>加载更多订阅项（已显示 {Math.min(routeLimit, platform.routes.length)}/{platform.routes.length}）</button>}
      {platformResults.length === 0 && curated.length === 0 && <p role="status" className="text-muted">{term ? '没有找到匹配的平台、订阅项或精选源。' : '这个分类暂无收录。'}</p>}
    </> : <div className="source-directory-grid">{categories.map(item => <button type="button" className="source-directory-category" key={item.id} aria-label={`${item.name} ${item.platforms.length} 个平台 · ${item.routeCount} 个订阅项 · ${item.sources.length} 个精选源`} onClick={() => openCategory(item.id)}>
      <span aria-hidden="true" className="source-directory-emoji">{item.emoji}</span>
      <strong>{item.name}</strong>
      <span className="text-muted text-sm">{item.platforms.length} 个平台 · {item.routeCount} 个订阅项{item.sources.length > 0 && ` · ${item.sources.length} 个精选源`}</span>
      <span className="text-muted text-sm source-directory-description">{item.description}</span>
    </button>)}</div>}
  </section>
}

function RouteList({ platform, routes, onConfigure }: { platform: DirectoryPlatform; routes: DirectoryRoute[]; onConfigure: (id: string) => void }) {
  return <div className="source-directory-routes">{routes.map(route => <article className="source-directory-source" key={route.id}>
    <div className="source-directory-source-copy"><h4>{route.name}</h4><p className="text-muted text-sm">rsshub://{platform.id}{route.path}</p>
      {route.requires_config && <span className="source-directory-badge">需要服务配置</span>}
      {route.anti_crawler && <span className="source-directory-badge">可能受源站访问限制</span>}
    </div>
    <button type="button" className="secondary" aria-label={`配置 ${route.name}`} onClick={() => onConfigure(route.id)}>配置订阅</button>
  </article>)}</div>
}
