import { findLocalAIPreset } from './localAIPresets'

// This module deliberately has no dependency on the site's authenticated HTTP client.
export interface LocalAIConfig {
  api_key: string
  base_url: string
  model: string
  remember?: boolean
}

export function localAIAccount(): string {
  try {
    const id = JSON.parse(localStorage.getItem('user') || 'null')?.id
    if (Number.isSafeInteger(id) && id > 0) return String(id)
  } catch { /* signed-out or invalid account */ }
  throw new Error('请先登录后配置本地 AI')
}
const storageKey = () => `rss-pal:local-ai:v1:${localAIAccount()}`

export function readLocalAIConfig(): LocalAIConfig | null {
  try {
    const key = storageKey()
    const raw = sessionStorage.getItem(key) || localStorage.getItem(key)
    if (!raw) return null
    const cfg = JSON.parse(raw)
    if (typeof cfg.api_key !== 'string' || !cfg.api_key || typeof cfg.base_url !== 'string' || typeof cfg.model !== 'string') return null
    return cfg
  } catch { return null }
}

export function validateLocalAIConfig(cfg: LocalAIConfig): LocalAIConfig {
  let url: URL
  try { url = new URL(cfg.base_url.trim()) } catch { throw new Error('请输入完整的 HTTPS API 地址') }
  const host = url.hostname.toLowerCase()
  if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash ||
      url.origin === window.location.origin || host === 'localhost' || host.endsWith('.localhost') ||
      host.endsWith('.local') || host.startsWith('[') || /^\d+\.\d+\.\d+\.\d+$/.test(host)) {
    throw new Error('请使用外部供应商的 HTTPS API 地址，不能包含账号密码、查询参数或本机地址')
  }
  if (!cfg.api_key.trim()) throw new Error('请输入 API Key')
  if (!cfg.model.trim()) throw new Error('请输入模型名称')
  return { api_key: cfg.api_key.trim(), base_url: url.href.replace(/\/+$/, ''), model: cfg.model.trim() }
}

export function resolveLocalAIConfig(cfg: LocalAIConfig): LocalAIConfig {
  const existing = readLocalAIConfig()
  const baseURL = cfg.base_url.trim().replace(/\/+$/, '')
  if (!cfg.api_key.trim() && existing && existing.base_url !== baseURL) {
    throw new Error('更换 API 地址时，请重新输入该供应商的 API Key')
  }
  return validateLocalAIConfig({ ...cfg, api_key: cfg.api_key || existing?.api_key || '' })
}

export function saveLocalAIConfig(cfg: LocalAIConfig, remember: boolean): void {
  const validated = resolveLocalAIConfig(cfg)
  const key = storageKey()
  // Write first: quota/security failures must not silently discard the prior key.
  const target = remember ? localStorage : sessionStorage
  target.setItem(key, JSON.stringify({ ...validated, remember }))
  ;(remember ? sessionStorage : localStorage).removeItem(key)
  window.dispatchEvent(new Event('local-ai-config-changed'))
}

export function clearLocalAIConfig(): void {
  const key = storageKey()
  localStorage.removeItem(key)
  sessionStorage.removeItem(key)
  window.dispatchEvent(new Event('local-ai-config-changed'))
}

export function clearLocalAISession(): void {
  try { sessionStorage.removeItem(storageKey()) } catch { /* already signed out */ }
}

export async function callLocalAI(config: LocalAIConfig, prompt: string, signal?: AbortSignal, test = false): Promise<string> {
  const cfg = validateLocalAIConfig(config)
  const nativeClaude = findLocalAIPreset(cfg.base_url)?.protocol === 'anthropic'
  signal?.throwIfAborted()
  const ctrl = new AbortController()
  const abort = () => ctrl.abort()
  signal?.addEventListener('abort', abort, { once: true })
  const timer = setTimeout(abort, test ? 30000 : 180000)
  try {
    let response: Response
    try {
      response = await fetch(`${cfg.base_url}/${nativeClaude ? 'messages' : 'chat/completions'}`, {
        method: 'POST', credentials: 'omit', redirect: 'error', referrerPolicy: 'no-referrer',
        headers: nativeClaude ? {
          'Content-Type': 'application/json', 'x-api-key': cfg.api_key,
          'anthropic-version': '2023-06-01', 'anthropic-dangerous-direct-browser-access': 'true',
        } : { 'Content-Type': 'application/json', Authorization: `Bearer ${cfg.api_key}` },
        body: JSON.stringify({ model: cfg.model, messages: [{ role: 'user', content: prompt }], max_tokens: test ? 16 : 8192, stream: false }),
        signal: ctrl.signal,
      })
    } catch {
      signal?.throwIfAborted()
      if (ctrl.signal.aborted) throw new Error('AI 请求超时，请手动重试')
      throw new Error('无法直连 AI 服务：请检查网络、地址和供应商是否允许浏览器跨域请求（CORS）；本站不会通过服务器代理 Key')
    }
    if (!response.ok) throw new Error(`AI 服务返回 HTTP ${response.status}，请检查 Key、模型权限或供应商额度`)
    let data
    try { data = await response.json() } catch { throw new Error('AI 服务返回了无效响应') }
    const choice = data?.choices?.[0]
    const content = nativeClaude
      ? (Array.isArray(data?.content) ? data.content.filter((block: { type?: string; text?: unknown }) => block.type === 'text' && typeof block.text === 'string').map((block: { text: string }) => block.text).join('\n') : '')
      : choice?.message?.content
    const complete = nativeClaude ? ['end_turn', 'stop_sequence'].includes(data?.stop_reason) : choice?.finish_reason === 'stop'
    if (typeof content !== 'string' || !content.trim()) throw new Error('AI 服务返回了空结果，请检查模型或重试')
    if (!test && !complete) throw new Error('AI 未返回完整结果，本次不会保存，请手动重试')
    // A provider can echo request credentials. Never upload or display that echo.
    if (content.includes(cfg.api_key)) throw new Error('AI 响应包含敏感凭证，本次结果已丢弃')
    return content.trim()
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener('abort', abort)
  }
}
