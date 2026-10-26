import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import LocalAISettings from '../src/components/LocalAISettings'
import { readLocalAIConfig, saveLocalAIConfig } from '../src/lib/localAI'

beforeEach(() => { localStorage.clear(); sessionStorage.clear(); localStorage.setItem('user', JSON.stringify({ id: 7 })) })
afterEach(() => vi.unstubAllGlobals())
it('saves a key locally without any request and explains manual generation', () => {
  const fetch = vi.fn(); vi.stubGlobal('fetch', fetch)
  render(<LocalAISettings />)
  fireEvent.change(screen.getByLabelText('API Key'), { target: { value: 'local-secret' } })
  fireEvent.change(screen.getByLabelText(/Base URL/), { target: { value: 'https://provider.example/v1' } })
  fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'model' } })
  fireEvent.click(screen.getByText('保存到本机'))
  expect(readLocalAIConfig()?.api_key).toBe('local-secret')
  expect(screen.getByRole('status').textContent).toContain('手动点击')
  expect(fetch).not.toHaveBeenCalled()
  expect((screen.getByLabelText(/API Key/) as HTMLInputElement).value).toBe('')
})
it('tests a saved key only when clicked and can delete it', async () => {
  saveLocalAIConfig({ api_key: 'local-secret', base_url: 'https://provider.example/v1', model: 'model' }, true)
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ choices: [{ message: { content: 'OK' }, finish_reason: 'stop' }] })))
  vi.stubGlobal('fetch', fetch)
  render(<LocalAISettings />)
  expect(fetch).not.toHaveBeenCalled()
  fireEvent.click(screen.getByText('测试连通性'))
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('测试通过'))
  expect(fetch).toHaveBeenCalledTimes(1)
  fireEvent.click(screen.getByText('清除本机配置'))
  expect(readLocalAIConfig()).toBeNull()
})
