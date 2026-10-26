import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { readLocalAIConfig, saveLocalAIConfig, clearLocalAIConfig, callLocalAI } from '../src/lib/localAI'

const cfg = { api_key: 'test-local-secret', base_url: 'https://provider.example/v1', model: 'test-model' }
beforeEach(() => {
  localStorage.clear(); sessionStorage.clear()
  localStorage.setItem('user', JSON.stringify({ id: 7 }))
})
afterEach(() => vi.unstubAllGlobals())
describe('local AI credentials', () => {
  it('stores temporary keys only in this session and makes no request', () => {
    const fetch = vi.fn(); vi.stubGlobal('fetch', fetch)
    saveLocalAIConfig(cfg, false)
    expect(readLocalAIConfig()).toEqual({ ...cfg, remember: false })
    expect(JSON.stringify(localStorage)).not.toContain(cfg.api_key)
    expect(fetch).not.toHaveBeenCalled()
  })
  it('isolates remembered keys by account and clears both storage modes', () => {
    saveLocalAIConfig(cfg, true)
    localStorage.setItem('user', JSON.stringify({ id: 8 }))
    expect(readLocalAIConfig()).toBeNull()
    localStorage.setItem('user', JSON.stringify({ id: 7 }))
    expect(readLocalAIConfig()?.api_key).toBe(cfg.api_key)
    clearLocalAIConfig()
    expect(readLocalAIConfig()).toBeNull()
    expect(JSON.stringify(localStorage) + JSON.stringify(sessionStorage)).not.toContain(cfg.api_key)
  })
  it('requires a new key when changing endpoint', () => {
    saveLocalAIConfig(cfg, true)
    expect(() => saveLocalAIConfig({ ...cfg, api_key: '', base_url: 'https://other.example/v1' }, true)).toThrow(/重新输入/)
    expect(readLocalAIConfig()?.base_url).toBe(cfg.base_url)
  })
  it('preserves the saved key when only model changes', () => {
    saveLocalAIConfig(cfg, true)
    saveLocalAIConfig({ ...cfg, api_key: '', model: 'other' }, false)
    expect(readLocalAIConfig()).toEqual({ ...cfg, model: 'other', remember: false })
  })
  it.each(['http://provider.example/v1', '/api', 'https://u:p@provider.example/v1', 'https://127.0.0.1/v1'])('rejects unsafe target %s', base_url => {
    expect(() => saveLocalAIConfig({ ...cfg, base_url }, false)).toThrow()
  })
})
describe('browser direct calls', () => {
  it('sends only the provider key, without site credentials or redirect following', async () => {
    localStorage.setItem('token', 'site-jwt')
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ choices: [{ message: { content: 'summary' }, finish_reason: 'stop' }] })))
    vi.stubGlobal('fetch', fetch)
    expect(await callLocalAI(cfg, 'article prompt')).toBe('summary')
    expect(fetch).toHaveBeenCalledWith('https://provider.example/v1/chat/completions', expect.objectContaining({
      credentials: 'omit', redirect: 'error', referrerPolicy: 'no-referrer',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${cfg.api_key}` },
    }))
    expect(JSON.stringify(fetch.mock.calls)).not.toContain('site-jwt')
  })
  it('gives actionable CORS guidance without leaking upstream errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError(cfg.api_key)))
    await expect(callLocalAI(cfg, 'prompt')).rejects.toThrow(/CORS/)
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => Promise.resolve(new Response(cfg.api_key, { status: 401 }))))
    await expect(callLocalAI(cfg, 'prompt')).rejects.toThrow('401')
    try { await callLocalAI(cfg, 'prompt') } catch (e) { expect(String(e)).not.toContain(cfg.api_key) }
  })
  it.each(['length', 'content_filter', null])('refuses incomplete output with finish reason %s', async finish_reason => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ choices: [{ message: { content: 'partial' }, finish_reason }] }))))
    await expect(callLocalAI(cfg, 'prompt')).rejects.toThrow(/完整/)
  })
  it('refuses empty output', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ choices: [{ message: { content: '  ' }, finish_reason: 'stop' }] }))))
    await expect(callLocalAI(cfg, 'prompt')).rejects.toThrow(/空/)
  })
  it('does not send anything after cancellation', async () => {
    const fetch = vi.fn(); vi.stubGlobal('fetch', fetch)
    const ctrl = new AbortController(); ctrl.abort()
    await expect(callLocalAI(cfg, 'prompt', ctrl.signal)).rejects.toThrow()
    expect(fetch).not.toHaveBeenCalled()
  })
})
describe('native Claude browser calls', () => {
  const claude={...cfg,base_url:'https://api.anthropic.com/v1',model:'claude-test'}
  it('uses the Messages protocol and direct-browser headers only on the official Claude origin', async () => {
    const fetch=vi.fn().mockResolvedValue(new Response(JSON.stringify({content:[{type:'thinking',thinking:'private reasoning'},{type:'text',text:'Claude summary'}],stop_reason:'end_turn'})))
    vi.stubGlobal('fetch',fetch)
    expect(await callLocalAI(claude,'article prompt')).toBe('Claude summary')
    expect(fetch).toHaveBeenCalledWith('https://api.anthropic.com/v1/messages',expect.objectContaining({
      credentials:'omit',redirect:'error',headers:{'Content-Type':'application/json','x-api-key':cfg.api_key,'anthropic-version':'2023-06-01','anthropic-dangerous-direct-browser-access':'true'},
    }))
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toMatchObject({model:'claude-test',messages:[{role:'user',content:'article prompt'}],stream:false})
    expect(fetch.mock.calls[0][1].headers.Authorization).toBeUndefined()
  })
  it('rejects truncated Claude responses', async () => {
    vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({content:[{type:'text',text:'partial'}],stop_reason:'max_tokens'}))))
    await expect(callLocalAI(claude,'prompt')).rejects.toThrow(/完整/)
  })
  it('never sends Claude headers to a similar-looking custom origin', async () => {
    const fetch=vi.fn().mockResolvedValue(new Response(JSON.stringify({choices:[{message:{content:'ok'},finish_reason:'stop'}]})))
    vi.stubGlobal('fetch',fetch)
    await callLocalAI({...claude,base_url:'https://api.anthropic.com.example/v1'},'prompt')
    expect(fetch.mock.calls[0][0]).toBe('https://api.anthropic.com.example/v1/chat/completions')
    expect(fetch.mock.calls[0][1].headers['x-api-key']).toBeUndefined()
  })
})
