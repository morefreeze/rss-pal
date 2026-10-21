import { afterEach, describe, expect, it, vi } from 'vitest'
import { generateSummaryStream } from '../src/api/client'

function response(lines: string) {
  return new Response(new ReadableStream({ start(c) { c.enqueue(new TextEncoder().encode(lines)); c.close() } }))
}
afterEach(() => vi.unstubAllGlobals())
describe('summary stream completion', () => {
  it('reports a clean EOF without a committed done frame as interrupted', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response('{"type":"brief_delta","text":"预览"}\n')))
    const onError = vi.fn(), onDone = vi.fn()
    await generateSummaryStream(1, undefined, { onError, onDone })
    expect(onError).toHaveBeenCalledWith(expect.stringContaining('中断'))
    expect(onDone).not.toHaveBeenCalled()
  })
  it('handles terminal errors once and ignores any later done', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response('{"type":"error","msg":"预算已用完"}\n{"type":"done"}\n')))
    const onError = vi.fn(), onDone = vi.fn()
    await generateSummaryStream(1, undefined, { onError, onDone })
    expect(onError).toHaveBeenCalledTimes(1)
    expect(onDone).not.toHaveBeenCalled()
  })
  it('extracts a readable HTTP error instead of raw JSON', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"error":"正在生成，请稍后查看"}', { status: 409 })))
    const onError = vi.fn()
    await generateSummaryStream(1, undefined, { onError })
    expect(onError).toHaveBeenCalledWith('正在生成，请稍后查看')
  })
  it('accepts a final done frame without a trailing newline', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response('{"type":"done"}')))
    const onError = vi.fn(), onDone = vi.fn()
    await generateSummaryStream(1, undefined, { onError, onDone })
    expect(onDone).toHaveBeenCalledTimes(1)
    expect(onError).not.toHaveBeenCalled()
  })
})
