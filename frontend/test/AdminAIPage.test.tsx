import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { AxiosError } from 'axios'
import AdminAIPage from '../src/pages/AdminAIPage'
import { getAdminAI, getAdminAIModels, saveAdminAI, type AdminAIConfig } from '../src/api/adminAI'
vi.mock('../src/api/adminAI', () => ({ getAdminAI: vi.fn(), getAdminAIModels: vi.fn(), saveAdminAI: vi.fn() }))
const config: AdminAIConfig = {
  revision: 3, threshold: 10000,
  small: { provider: 'zai', model: 'glm-small' }, large: { provider: 'zai', model: 'glm-large' },
  providers: [
    { id: 'zai', name: '智谱', endpoint: 'global', has_key: true, endpoints: [{ id: 'global', name: '国际' }, { id: 'cn', name: '中国' }] },
    { id: 'openai', name: 'OpenAI', endpoint: 'global', has_key: false, endpoints: [{ id: 'global', name: '国际' }] },
  ],
}
const catalog = { models: [{ id: 'glm-small', name: 'GLM Small' }, { id: 'glm-large', name: 'GLM Large' }], source: 'upstream' }
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }
async function mount() { render(<AdminAIPage user={{ is_admin: true }} />); await screen.findByLabelText('字符数'); await waitFor(() => expect(screen.queryByText('正在连接模型服务…')).toBeNull()) }
beforeEach(() => {
  vi.mocked(getAdminAI).mockReset().mockResolvedValue(structuredClone(config))
  vi.mocked(getAdminAIModels).mockReset().mockResolvedValue(catalog)
  vi.mocked(saveAdminAI).mockReset().mockResolvedValue({ ...config, revision: 4 })
})
it('preserves defaults and can save when the model service fails', async () => {
  vi.mocked(getAdminAIModels).mockRejectedValue(new Error('unavailable'))
  await mount()
  expect((screen.getAllByLabelText('模型')[0] as HTMLSelectElement).value).toBe('glm-small')
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() => expect(saveAdminAI).toHaveBeenCalledWith({ revision: 3, threshold: 10000, small: config.small, large: config.large, credentials: {} }))
  await screen.findByText('配置已保存')
})
it('invalidates both route selections on endpoint change and ignores an old response', async () => {
  await mount()
  const old = deferred<typeof catalog>()
  vi.mocked(getAdminAIModels).mockReturnValueOnce(old.promise)
  fireEvent.click(screen.getAllByRole('button', { name: '刷新模型列表' })[0])
  fireEvent.change(screen.getByLabelText('服务区域 / 接口'), { target: { value: 'cn' } })
  await act(async () => { old.resolve({ models: [{ id: 'stale-model', name: 'Stale' }], source: 'upstream' }) })
  expect(screen.queryByRole('option', { name: 'Stale' })).toBeNull()
  for (const select of screen.getAllByLabelText('模型')) expect((select as HTMLSelectElement).value).toBe('')
  expect((screen.getByRole('button', { name: '保存配置' }) as HTMLButtonElement).disabled).toBe(true)
})
it('does not send draft keys while typing and never transfers a model across companies', async () => {
  await mount()
  fireEvent.change(screen.getByLabelText('配置公司'), { target: { value: 'openai' } })
  const calls = vi.mocked(getAdminAIModels).mock.calls.length
  fireEvent.change(screen.getByLabelText('API Key'), { target: { value: 'draft-secret' } })
  expect(getAdminAIModels).toHaveBeenCalledTimes(calls)
  fireEvent.change(screen.getAllByLabelText('公司')[0], { target: { value: 'openai' } })
  await waitFor(() => expect(getAdminAIModels).toHaveBeenLastCalledWith({ provider: 'openai', endpoint: 'global', api_key: 'draft-secret' }, expect.any(AbortSignal)))
  expect((screen.getAllByLabelText('模型')[0] as HTMLSelectElement).value).toBe('')
  expect((screen.getAllByLabelText('模型')[1] as HTMLSelectElement).value).toBe('glm-large')
})
it('requires explicit reload after a revision conflict and retains the draft', async () => {
  await mount()
  const conflict = new AxiosError('conflict')
  conflict.response = { status: 409 } as typeof conflict.response
  vi.mocked(saveAdminAI).mockRejectedValue(conflict)
  fireEvent.change(screen.getByLabelText('字符数'), { target: { value: '12345' } })
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
  await screen.findByText(/配置已被其他管理员修改/)
  expect((screen.getByLabelText('字符数') as HTMLInputElement).value).toBe('12345')
  expect(getAdminAI).toHaveBeenCalledTimes(1)
  expect((screen.getByRole('button', { name: '保存配置' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: '重新加载配置' }))
  await waitFor(() => expect(getAdminAI).toHaveBeenCalledTimes(2))
  await waitFor(() => expect((screen.getByLabelText('字符数') as HTMLInputElement).value).toBe('10000'))
})
it('enforces the server threshold maximum and focuses credentials for the selected company', async () => {
  await mount()
  const threshold = screen.getByLabelText('字符数') as HTMLInputElement
  expect(threshold.max).toBe('100000')
  fireEvent.change(threshold, { target: { value: '100001' } })
  expect((screen.getByRole('button', { name: '保存配置' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(threshold, { target: { value: '100000' } })
  expect((screen.getByRole('button', { name: '保存配置' }) as HTMLButtonElement).disabled).toBe(false)
  fireEvent.change(screen.getAllByLabelText('公司')[1], { target: { value: 'openai' } })
  expect((screen.getByLabelText('配置公司') as HTMLSelectElement).value).toBe('openai')
  await waitFor(() => expect(screen.queryByText('正在连接模型服务…')).toBeNull())
})
it('explains that switching endpoints requires a new key and displays sanitized API errors', async () => {
  await mount()
  fireEvent.change(screen.getByLabelText('服务区域 / 接口'), { target: { value: 'cn' } })
  expect(screen.getByText(/已更换接入方式，必须输入对应的新 API Key/)).toBeTruthy()
  expect((screen.getByLabelText('API Key') as HTMLInputElement).placeholder).toBe('输入新接入方式对应的 API Key')
  const failure = new AxiosError('bad request')
  failure.response = { status: 400, data: { error: '更换接入方式时必须提供新的 API Key' } } as typeof failure.response
  vi.mocked(getAdminAIModels).mockRejectedValue(failure)
  fireEvent.click(screen.getByRole('button', { name: '使用此凭据获取模型' }))
  expect((await screen.findAllByText('更换接入方式时必须提供新的 API Key')).length).toBeGreaterThan(0)
})
