import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { api, generateSummaryStream } from '../src/api/client'
import { saveLocalAIConfig } from '../src/lib/localAI'
const cfg = { api_key: 'local-secret', base_url: 'https://provider.example/v1', model: 'model' }
beforeEach(() => {
  localStorage.clear(); sessionStorage.clear();localStorage.setItem('user', JSON.stringify({ id: 7 }));localStorage.setItem('token', 'site-jwt')
  saveLocalAIConfig(cfg, true)
})
afterEach(() => vi.unstubAllGlobals())
const output = (content: string, finish_reason = 'stop') => new Response(JSON.stringify({ choices: [{ message: { content }, finish_reason }] }))
it('saves only finished results and calls done after the server acknowledges them', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({ data: { brief_prompt: 'brief input', detailed_prompt: 'detailed input', content_hash: 'hash', summary_version: 'version' } })
  const put = vi.spyOn(api, 'put').mockResolvedValue({ data: {} })
  const fetch = vi.fn().mockResolvedValueOnce(output('brief')).mockResolvedValueOnce(output('detailed'))
  vi.stubGlobal('fetch', fetch)
  const onDone = vi.fn(() => expect(put).toHaveBeenCalledTimes(1)), onError = vi.fn()
  await generateSummaryStream(12, 3, { onDone, onError })
  expect(get).toHaveBeenCalledWith('/articles/12/summary/input', expect.objectContaining({ params: { template_id: 3 } }))
  expect(put).toHaveBeenCalledWith('/articles/12/summary/result', { content_hash: 'hash', summary_version: 'version', summary_brief: 'brief', summary_detailed: 'detailed' }, expect.anything())
  expect(JSON.stringify(put.mock.calls)).not.toContain(cfg.api_key)
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(onDone).toHaveBeenCalledTimes(1);expect(onError).not.toHaveBeenCalled()
})
it('does not save partial output or fall back to server AI', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({ data: { brief_prompt: 'brief', detailed_prompt: 'detailed', content_hash: 'hash', summary_version: 'version' } })
  const put = vi.spyOn(api, 'put'), post = vi.spyOn(api, 'post')
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(output('partial', 'length')))
  const onDone=vi.fn(), onError=vi.fn()
  await generateSummaryStream(12, undefined, { onDone, onError })
  expect(onError).toHaveBeenCalled();expect(onDone).not.toHaveBeenCalled();expect(put).not.toHaveBeenCalled();expect(post).not.toHaveBeenCalled()
})
it('does not upload a result after the account changes', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({ data: { brief_prompt: 'brief', detailed_prompt: 'detailed', content_hash: 'hash', summary_version: 'version' } })
  const put=vi.spyOn(api, 'put')
  vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => { localStorage.setItem('user', JSON.stringify({ id: 8 })); return output('brief') }))
  await generateSummaryStream(12, undefined, { onError: vi.fn() })
  expect(put).not.toHaveBeenCalled()
})
