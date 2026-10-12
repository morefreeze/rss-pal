import { afterEach, expect, it, vi } from 'vitest'
import { AxiosError } from 'axios'
import { api, fetchContent } from '../src/api/client'

afterEach(() => { vi.useRealTimers() })

it('allows a manual content response after the ordinary ten-second request deadline', async () => {
  vi.useFakeTimers()
  const original = api.defaults.adapter
  api.defaults.adapter = config => new Promise((resolve, reject) => {
    const deadline = setTimeout(() => reject(new AxiosError('timeout', 'ECONNABORTED', config)), config.timeout)
    setTimeout(() => {
      clearTimeout(deadline)
      resolve({ data: { content: 'Full article including the final paragraph' }, status: 200, statusText: 'OK', headers: {}, config })
    }, 11000)
  })
  try {
    const outcome = fetchContent(3145).catch(error => error)
    await vi.advanceTimersByTimeAsync(11000)
    expect(await outcome).toEqual({ content: 'Full article including the final paragraph' })
  } finally { api.defaults.adapter = original }
})
