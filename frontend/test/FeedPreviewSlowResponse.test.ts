// @vitest-environment node
import { createServer } from 'node:http'
import { expect, it, vi } from 'vitest'
import { api, previewFeed } from '../src/api/client'

it('accepts a valid preview taking longer than the generic ten-second API budget', async () => {
  vi.stubGlobal('localStorage', { getItem: () => null })
  const response = { feed_title: 'Slow but valid RSS', feed_type: 'rss', items: [] }
  const server = createServer((_request, reply) => {
    setTimeout(() => { reply.setHeader('Content-Type', 'application/json'); reply.end(JSON.stringify(response)) }, 11000)
  })
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
  const address = server.address() as { port: number }
  const oldBase = api.defaults.baseURL
  api.defaults.baseURL = `http://127.0.0.1:${address.port}/api`
  try {
    expect(await previewFeed('https://example.com/rss')).toEqual(response)
  } finally {
    api.defaults.baseURL = oldBase
    server.closeAllConnections()
    await new Promise<void>(resolve => server.close(() => resolve()))
    vi.unstubAllGlobals()
  }
}, 16000)
