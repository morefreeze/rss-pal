import { useState } from 'react'
import type { DirectoryPlatform, DirectoryRoute } from '../api/sourceDirectory'
import { buildDirectoryRouteURL, manualDirectoryRouteURL, parseDirectoryRoute, publicDirectoryFeedURL } from '../utils/directoryRoute'

interface Props {
  platform: DirectoryPlatform
  route: DirectoryRoute
  onPreview: (url: string) => void
  onBack: () => void
}
const sensitiveParameter = /cookie|token|secret|password|credential|api.?key/i

function examplePath(namespace: string, example: string): string {
  try {
    const publicURL = publicDirectoryFeedURL(example)
    const path = publicURL && new URL(publicURL).origin === 'https://rsshub.app' ? new URL(publicURL).pathname : example
    manualDirectoryRouteURL(namespace, path)
    return path
  } catch { return '' }
}

export default function DirectoryRouteForm(props: Props) {
  // Remount local state when the selected platform/route changes.
  return <RouteForm key={`${props.platform.id}:${props.route.id}`} {...props} />
}
function RouteForm({ platform, route, onPreview, onBack }: Props) {
  const paths = [...new Set([route.path, ...(route.paths ?? [])])]
  const [path, setPath] = useState(route.path)
  const parsed = parseDirectoryRoute(path)
  const credentialRoute = Object.keys(route.parameters).some(name => sensitiveParameter.test(name)) || paths.some(pattern => [...pattern.matchAll(/:([A-Za-z0-9_]+)/g)].some(match => sensitiveParameter.test(match[1])))
  const [manual, setManual] = useState(parsed.complex)
  const [fullPath, setFullPath] = useState(() => parsed.complex ? examplePath(platform.id, route.example) : '')
  const [values, setValues] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const example = examplePath(platform.id, route.example)
  const feeds = (credentialRoute ? [] : route.top_feeds).flatMap(feed => {
    const url = publicDirectoryFeedURL(feed.url)
    return url ? [{ ...feed, url }] : []
  })
  const choosePath = (next: string) => {
    const complex = parseDirectoryRoute(next).complex
    setPath(next)
    setValues({})
    setManual(complex)
    setFullPath(complex ? example : '')
    setError('')
  }
  const preview = () => {
    try {
      if (credentialRoute) throw new Error('此路由涉及凭据，请先阅读 RSSHub 文档并在服务端配置；此处不收集密钥。')
      const url = manual ? manualDirectoryRouteURL(platform.id, fullPath) : buildDirectoryRouteURL(platform.id, { ...route, path }, values)
      setError('')
      onPreview(url)
    } catch (cause) { setError(cause instanceof Error ? cause.message : '请检查路由参数') }
  }
  return <section className="directory-route-form" aria-label="配置订阅路由">
    <button type="button" className="btn btn-secondary" onClick={onBack}>← 返回路由列表</button>
    <h3>{platform.name} · {route.name}</h3>
    {route.description && <p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{route.description}</p>}
    <p><code>{`/${platform.id}${path}`}</code></p>
    {route.requires_config && <p>此路由需要 RSSHub 服务端配置{route.config_names.length ? `：${route.config_names.join('、')}` : ''}。公共实例可能无法提供内容。</p>}
    {route.anti_crawler && <p>此路由可能受网站反爬限制，预览结果取决于公共实例可用性。</p>}
    <p>只填写公开内容标识，请勿输入 Cookie、API 密钥或其他凭据。预览后可确认订阅。</p>
    <a href="https://docs.rsshub.app/" target="_blank" rel="noopener noreferrer">查看 RSSHub 文档</a>
    {paths.length > 1 && <label style={{ display: 'block', marginTop: 16 }}>路径模式<select aria-label="路径模式" value={path} onChange={event => choosePath(event.target.value)}>{paths.map(item => <option key={item} value={item}>{item}</option>)}</select></label>}
    {!credentialRoute && !parsed.complex && <label style={{ display: 'block', marginTop: 16 }}><input type="checkbox" checked={manual} onChange={event => { setManual(event.target.checked); setError('') }} />编辑完整路径</label>}
    {parsed.complex && <p>此路由使用复杂路径规则。请根据文档填写完整、具体的公开路由路径。</p>}
    <form onSubmit={event => { event.preventDefault(); preview() }}>
      {credentialRoute ? <p>此路由涉及凭据，此处不收集密钥，也不支持通过完整路径或示例预览。请阅读服务端配置文档。</p> : manual ? <label style={{ display: 'block', marginTop: 16 }}>完整 RSSHub 路径<input style={{ display: 'block', width: '100%' }} aria-label="完整 RSSHub 路径" value={fullPath} placeholder={`/${platform.id}/…`} onChange={event => setFullPath(event.target.value)} autoComplete="off" /></label> : parsed.parameters.map(parameter => {
        const detail = route.parameters[parameter.name]
        const label = `${parameter.name}（${parameter.optional ? '可选' : '必填'}）`
        const value = values[parameter.name] ?? detail?.default ?? ''
        const secret = sensitiveParameter.test(parameter.name)
        return <label key={parameter.name} style={{ display: 'block', marginTop: 16 }}>{label}
          {detail?.description && <span style={{ display: 'block', whiteSpace: 'pre-wrap' }}>{detail.description}</span>}
          {secret ? <span style={{ display: 'block' }}>此参数涉及凭据，请阅读服务端配置文档。</span> : detail?.options?.length ? <select aria-label={label} value={value} onChange={event => setValues(previous => ({ ...previous, [parameter.name]: event.target.value }))}>
            <option value="">{parameter.optional ? '不填写' : '请选择'}</option>
            {value && !detail.options.some(option => option.value === value) && <option value={value}>{value}</option>}
            {detail.options.map((option, index) => <option key={`${option.value}:${index}`} value={option.value}>{option.label}</option>)}
          </select> : <input style={{ display: 'block', width: '100%' }} aria-label={label} value={value} onChange={event => setValues(previous => ({ ...previous, [parameter.name]: event.target.value }))} autoComplete="off" />}
        </label>
      })}
      {!credentialRoute && example && <button style={{ marginTop: 16 }} type="button" className="btn btn-secondary" onClick={() => { setFullPath(example); setManual(true); setError('') }}>填入示例路径</button>}
      {error && <p role="alert">{error}</p>}
      <button style={{ marginTop: 16 }} className="btn btn-primary" type="submit" disabled={credentialRoute}>预览订阅</button>
    </form>
    {feeds.length > 0 && <div><h4>公开订阅示例</h4>{feeds.map((feed, index) => <button type="button" className="btn btn-secondary" key={`${feed.url}:${index}`} onClick={() => onPreview(feed.url)}>预览示例：{feed.title || feed.url}</button>)}</div>}
  </section>
}
