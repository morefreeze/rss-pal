import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useConnectionKeepAlive } from '../src/hooks/useConnectionKeepAlive'

function setHidden(hidden: boolean) {
  Object.defineProperty(document, 'hidden', {
    configurable: true,
    get: () => hidden,
  })
  document.dispatchEvent(new Event('visibilitychange'))
}

function setConnection(connection: unknown) {
  Object.defineProperty(navigator, 'connection', {
    configurable: true,
    get: () => connection,
  })
}

describe('useConnectionKeepAlive', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.useFakeTimers()
    fetchMock = vi.fn(() => Promise.resolve(new Response('ok')))
    vi.stubGlobal('fetch', fetchMock)
    setHidden(false)
    setConnection(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('pings the origin on the configured interval while enabled and visible', () => {
    renderHook(() => useConnectionKeepAlive(true, 30_000))
    expect(fetchMock).not.toHaveBeenCalled()

    vi.advanceTimersByTime(30_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/health',
      expect.objectContaining({ method: 'GET' }),
    )

    vi.advanceTimersByTime(60_000)
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('never pings while disabled', () => {
    renderHook(() => useConnectionKeepAlive(false, 30_000))
    vi.advanceTimersByTime(120_000)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('pauses while the tab is hidden and resumes immediately on return', () => {
    renderHook(() => useConnectionKeepAlive(true, 30_000))

    vi.advanceTimersByTime(30_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    setHidden(true)
    vi.advanceTimersByTime(300_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // Returning to the tab pings at once, because the hidden gap is well past
    // the ~60s point where the connection dies.
    setHidden(false)
    expect(fetchMock).toHaveBeenCalledTimes(2)

    vi.advanceTimersByTime(30_000)
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('skips entirely when the client asks for reduced data', () => {
    setConnection({ saveData: true })
    renderHook(() => useConnectionKeepAlive(true, 30_000))
    vi.advanceTimersByTime(120_000)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('skips entirely on a 2g connection', () => {
    setConnection({ effectiveType: '2g' })
    renderHook(() => useConnectionKeepAlive(true, 30_000))
    vi.advanceTimersByTime(120_000)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('still pings on a fast connection that reports no constraints', () => {
    setConnection({ effectiveType: '4g', saveData: false })
    renderHook(() => useConnectionKeepAlive(true, 30_000))
    vi.advanceTimersByTime(30_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('stops pinging after unmount', () => {
    const { unmount } = renderHook(() => useConnectionKeepAlive(true, 30_000))
    vi.advanceTimersByTime(30_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    unmount()
    vi.advanceTimersByTime(300_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('swallows a failed ping without surfacing an unhandled rejection', async () => {
    fetchMock.mockRejectedValue(new Error('offline'))
    renderHook(() => useConnectionKeepAlive(true, 30_000))

    vi.advanceTimersByTime(30_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await vi.runOnlyPendingTimersAsync()
    // Reaching here without an unhandled rejection is the assertion.
  })
})
