import { useEffect, useRef, useState } from 'react'
import { isAxiosError } from 'axios'
import { getAdminAI, getAdminAIModels, saveAdminAI, type AdminAIConfig, type AICredential, type AIModel, type AISelection } from '../api/adminAI'
import './AdminAIPage.css'

type Size = 'small' | 'large'
type Catalog = { models: AIModel[]; loading: boolean; error: string; source?: string }
const emptyCatalog: Catalog = { models: [], loading: false, error: '' }
function errorMessage(error: unknown, models = false) {
  const status = isAxiosError(error) ? error.response?.status : undefined
  const detail = isAxiosError(error) ? error.response?.data?.error : undefined
  if ([400, 502, 503].includes(status ?? 0) && typeof detail === 'string' && detail.trim()) return detail
  if (status === 403) return '仅管理员可操作 AI 配置。'
  if (status === 400) return models ? '请检查该公司的 API Key 和服务区域后重试。' : '配置未通过校验，请检查阈值、模型和 API Key。'
  if (models && status === 502) return '模型服务暂不可用或 API Key 无效，请检查后重试。'
  return models ? '获取模型失败，请稍后重试。当前已保存的模型仍可保留。' : '操作失败，请稍后重试；未保存的修改已保留。'
}

export default function AdminAIPage({ user }: { user?: { is_admin: boolean } | null }) {
  const [saved, setSaved] = useState<AdminAIConfig | null>(null)
  const [routes, setRoutes] = useState<Record<Size, AISelection>>({ small: { provider: '', model: '' }, large: { provider: '', model: '' } })
  const [threshold, setThreshold] = useState('10000')
  const [credentials, setCredentials] = useState<Record<string, AICredential>>({})
  const credentialsRef = useRef(credentials)
  const [company, setCompany] = useState('')
  const [catalogs, setCatalogs] = useState<Record<string, Catalog>>({})
  const requests = useRef<Record<string, AbortController>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [conflict, setConflict] = useState(false)
  const [success, setSuccess] = useState(false)
  const [reload, setReload] = useState(0)

  function cancelCatalogs() {
    Object.values(requests.current).forEach(controller => controller.abort())
    requests.current = {}
  }
  async function loadModels(provider: string, credential = credentialsRef.current[provider]) {
    if (!credential) return
    requests.current[provider]?.abort()
    const controller = new AbortController()
    requests.current[provider] = controller
    setCatalogs(current => ({ ...current, [provider]: { ...(current[provider] || emptyCatalog), loading: true, error: '' } }))
    try {
      const result = await getAdminAIModels({ provider, endpoint: credential.endpoint, ...(credential.api_key.trim() ? { api_key: credential.api_key.trim() } : {}) }, controller.signal)
      if (controller.signal.aborted || requests.current[provider] !== controller) return
      setCatalogs(current => ({ ...current, [provider]: { models: result.models, source: result.source, loading: false, error: result.models.length ? '' : '该公司未返回可用模型，请检查服务区域或稍后刷新。' } }))
    } catch (err) {
      if (controller.signal.aborted || requests.current[provider] !== controller) return
      setCatalogs(current => ({ ...current, [provider]: { ...emptyCatalog, error: errorMessage(err, true) } }))
    }
  }
  function acceptConfig(config: AdminAIConfig) {
    cancelCatalogs()
    const next = Object.fromEntries(config.providers.map(provider => [provider.id, { endpoint: provider.endpoint, api_key: '' }]))
    credentialsRef.current = next
    setCredentials(next)
    setSaved(config)
    setRoutes({ small: config.small, large: config.large })
    setThreshold(String(config.threshold))
    setCompany(current => config.providers.some(provider => provider.id === current) ? current : config.small.provider)
    setCatalogs({})
    setConflict(false)
    return next
  }
  useEffect(() => {
    if (!user?.is_admin) { setLoading(false); return }
    const controller = new AbortController()
    setLoading(true)
    setError('')
    setSuccess(false)
    void getAdminAI(controller.signal).then(config => {
      if (controller.signal.aborted) return
      const next = acceptConfig(config)
      for (const provider of new Set([config.small.provider, config.large.provider])) void loadModels(provider, next[provider])
    }).catch(err => {
      if (!controller.signal.aborted) setError(errorMessage(err))
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => { controller.abort(); cancelCatalogs() }
  }, [user?.is_admin, reload])

  function changeProvider(size: Size, provider: string) {
    setCompany(provider)
    setRoutes(current => ({ ...current, [size]: { provider, model: '' } }))
    setSuccess(false)
    void loadModels(provider)
  }
  function changeCredential(provider: string, field: keyof AICredential, value: string) {
    const next = { ...credentialsRef.current, [provider]: { ...credentialsRef.current[provider], [field]: value } }
    credentialsRef.current = next
    setCredentials(next)
    requests.current[provider]?.abort()
    delete requests.current[provider]
    setCatalogs(current => ({ ...current, [provider]: emptyCatalog }))
    // A model belongs to its service region and credentials; require a fresh
    // selection after either changes, including both routes using this company.
    setRoutes(current => Object.fromEntries(Object.entries(current).map(([size, route]) => [size, route.provider === provider ? { ...route, model: '' } : route])) as Record<Size, AISelection>)
    setSuccess(false)
  }
  const changedCredentials = Object.fromEntries(Object.entries(credentials).filter(([id, credential]) => credential.api_key.trim() || credential.endpoint !== saved?.providers.find(provider => provider.id === id)?.endpoint))
  const dirty = !!saved && (threshold !== String(saved.threshold) || JSON.stringify(routes.small) !== JSON.stringify(saved.small) || JSON.stringify(routes.large) !== JSON.stringify(saved.large) || Object.keys(changedCredentials).length > 0)
  const validThreshold = /^\d+$/.test(threshold) && Number.isSafeInteger(Number(threshold)) && Number(threshold) > 0 && Number(threshold) <= 100000
  const modelsLoading = Object.values(catalogs).some(catalog => catalog.loading)
  async function save() {
    if (!saved) return
    setSaving(true)
    setError('')
    setSuccess(false)
    try {
      const config = await saveAdminAI({ revision: saved.revision, threshold: Number(threshold), ...routes, credentials: changedCredentials })
      acceptConfig(config)
      setSuccess(true)
    } catch (err) {
      if (isAxiosError(err) && err.response?.status === 409) {
        setConflict(true)
        setError('配置已被其他管理员修改。请重新加载最新配置后再编辑；重新加载会放弃本页未保存的修改。')
      } else setError(errorMessage(err))
    } finally { setSaving(false) }
  }

  if (!user?.is_admin) return <div className="card">仅管理员可访问 AI 模型配置</div>
  const selectedProvider = saved?.providers.find(provider => provider.id === company)
  const endpointChanged = !!selectedProvider && credentials[company]?.endpoint !== selectedProvider.endpoint
  return <div className="admin-ai">
    <header><h2>AI 模型配置</h2><p className="text-muted">按文章长度选择模型。配置保存后用于后续任务。</p></header>
    {loading && <p role="status">正在加载配置…</p>}
    {error && <div className="ai-notice ai-error" role="alert">{error}{(conflict || !saved) && <button type="button" disabled={loading || saving} onClick={() => setReload(value => value + 1)}>重新加载配置</button>}</div>}
    {saved && !loading && <form onSubmit={event => { event.preventDefault(); void save() }}>
      <fieldset disabled={saving} className="ai-fields">
        <section className="card ai-threshold"><h3>文章长度阈值</h3><label htmlFor="ai-threshold">字符数</label><input id="ai-threshold" type="number" min="1" max="100000" step="1" required value={threshold} onChange={event => { setThreshold(event.target.value); setSuccess(false) }} aria-describedby="ai-threshold-help" /><p id="ai-threshold-help" className="text-muted">范围 1–100,000 字符，默认 10,000。不超过阈值使用短文章模型，超过阈值使用长文章模型。</p></section>
        <div className="ai-route-grid">{(['small', 'large'] as Size[]).map(size => {
          const route = routes[size]
          const catalog = catalogs[route.provider] || emptyCatalog
          const fallback = route.model && !catalog.models.some(model => model.id === route.model)
          return <section className="card ai-route" key={size} aria-labelledby={`ai-${size}-title`}>
            <h3 id={`ai-${size}-title`}>{size === 'small' ? '短文章模型' : '长文章模型'}</h3>
            <label htmlFor={`ai-${size}-provider`}>公司</label><select id={`ai-${size}-provider`} value={route.provider} onChange={event => changeProvider(size, event.target.value)}>{saved.providers.map(provider => <option key={provider.id} value={provider.id}>{provider.name}</option>)}</select>
            <label htmlFor={`ai-${size}-model`}>模型</label><select id={`ai-${size}-model`} value={route.model} disabled={catalog.loading} required onChange={event => { setRoutes(current => ({ ...current, [size]: { ...current[size], model: event.target.value } })); setSuccess(false) }}>
              <option value="">{catalog.loading ? '正在获取模型…' : '请选择模型'}</option>
              {fallback && <option value={route.model}>{route.model}（当前配置）</option>}
              {catalog.models.map(model => <option key={model.id} value={model.id}>{model.name || model.id}</option>)}
            </select>
            <button className="secondary" type="button" disabled={catalog.loading} onClick={() => void loadModels(route.provider)}>{catalog.loading ? '正在获取…' : '刷新模型列表'}</button>
            {catalog.error && <p className="ai-notice ai-error" role="alert">{catalog.error}</p>}
            {catalog.loading && <p className="text-muted" role="status">正在连接模型服务…</p>}
            {!catalog.loading && !catalog.error && catalog.models.length > 0 && <p className="text-muted text-sm">已获取 {catalog.models.length} 个模型</p>}
          </section>
        })}</div>
        <section className="card ai-credentials"><h3>公司凭据</h3><p className="text-muted">各公司的 API Key 独立保存。输入后点击获取模型，保存配置后生效。</p>
          <label htmlFor="ai-credential-provider">配置公司</label><select id="ai-credential-provider" value={company} onChange={event => setCompany(event.target.value)}>{saved.providers.map(provider => <option key={provider.id} value={provider.id}>{provider.name}{provider.has_key ? ' · 已配置 Key' : ''}</option>)}</select>
          {selectedProvider && credentials[company] && <>
            <label htmlFor="ai-endpoint">服务区域 / 接口</label><select id="ai-endpoint" value={credentials[company].endpoint} onChange={event => changeCredential(company, 'endpoint', event.target.value)}>{selectedProvider.endpoints.map(endpoint => <option key={endpoint.id} value={endpoint.id}>{endpoint.name}</option>)}</select>
            <label htmlFor="ai-key">API Key</label><input id="ai-key" type="password" autoComplete="off" spellCheck={false} value={credentials[company].api_key} placeholder={endpointChanged ? '输入新接入方式对应的 API Key' : selectedProvider.has_key ? '已配置；留空保留现有 Key' : '输入该公司的 API Key'} onChange={event => changeCredential(company, 'api_key', event.target.value)} aria-describedby="ai-key-help" />
            <p id="ai-key-help" className="text-muted text-sm">{endpointChanged ? '已更换接入方式，必须输入对应的新 API Key，不能沿用已保存的 Key。' : selectedProvider.has_key ? '已有 Key 不会回显。' : '该公司尚未配置 Key。'}更换 Key 或服务区域后，需要重新获取并选择该公司的模型。</p>
            <button className="secondary" type="button" disabled={catalogs[company]?.loading} onClick={() => void loadModels(company)}>{catalogs[company]?.loading ? '正在获取…' : '使用此凭据获取模型'}</button>
            {catalogs[company]?.error && <p className="ai-notice ai-error" role="alert">{catalogs[company].error}</p>}
            {!!catalogs[company]?.models.length && <p role="status" className="text-muted">已获取 {catalogs[company].models.length} 个模型，可在上方选择。</p>}
          </>}
        </section>
      </fieldset>
      <div className="ai-save"><button type="submit" disabled={saving || modelsLoading || conflict || !validThreshold || !routes.small.model || !routes.large.model}>{saving ? '正在保存…' : '保存配置'}</button><span role="status">{success ? '配置已保存' : dirty ? '有未保存的修改' : '当前配置已保存'}</span></div>
    </form>}
  </div>
}
