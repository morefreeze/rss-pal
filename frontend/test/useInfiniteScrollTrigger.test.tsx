import { useRef } from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useInfiniteScrollTrigger } from '../src/hooks/useInfiniteScrollTrigger'

let intersectionObserverCallback: IntersectionObserverCallback | undefined
let intersectionObserverInstance: TestIntersectionObserver | undefined
let intersectionObserverOptions: IntersectionObserverInit | undefined

class TestIntersectionObserver implements IntersectionObserver {
  readonly root: Element | Document | null
  readonly rootMargin: string
  readonly thresholds = [0]
  observe: IntersectionObserver['observe'] = vi.fn()
  unobserve: IntersectionObserver['unobserve'] = vi.fn()
  disconnect: IntersectionObserver['disconnect'] = vi.fn()
  takeRecords: IntersectionObserver['takeRecords'] = vi.fn(() => [])

  constructor(callback: IntersectionObserverCallback, options: IntersectionObserverInit = {}) {
    intersectionObserverCallback = callback
    intersectionObserverInstance = this
    intersectionObserverOptions = options
    this.root = options.root ?? null
    this.rootMargin = options.rootMargin ?? '0px'
  }
}

function Harness({
  onLoadMore,
  enabled = true,
  refreshKey = 0,
}: {
  onLoadMore: () => Promise<void>
  enabled?: boolean
  refreshKey?: number
}) {
  const targetRef = useRef<HTMLDivElement>(null)

  useInfiniteScrollTrigger({
    targetRef,
    enabled,
    refreshKey,
    rootMarginPx: 200,
    onLoadMore,
  })

  return <div ref={targetRef} data-testid="infinite-scroll-target" />
}

describe('useInfiniteScrollTrigger', () => {
  let animationFrames: Map<number, FrameRequestCallback>
  let nextFrameId: number

  beforeEach(() => {
    animationFrames = new Map()
    nextFrameId = 1
    intersectionObserverCallback = undefined
    intersectionObserverInstance = undefined
    intersectionObserverOptions = undefined
    vi.stubGlobal('IntersectionObserver', TestIntersectionObserver)
    vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
      const frameId = nextFrameId++
      animationFrames.set(frameId, callback)
      return frameId
    }))
    vi.stubGlobal('cancelAnimationFrame', vi.fn((frameId: number) => {
      animationFrames.delete(frameId)
    }))
    vi.stubGlobal('innerHeight', 800)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  const flushAnimationFrames = () => {
    const queuedFrames = Array.from(animationFrames.values())
    animationFrames.clear()
    queuedFrames.forEach((callback) => callback(performance.now()))
  }

  const flushPromiseChain = async () => {
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
  }

  const waitForNextTask = () => new Promise<void>((resolve) => {
    setTimeout(resolve, 0)
  })

  it('loads when scrolling brings the target within the viewport margin', async () => {
    let targetTop = 10_000
    const onLoadMore = vi.fn(async () => {})
    render(<Harness onLoadMore={onLoadMore} />)

    const target = screen.getByTestId('infinite-scroll-target')
    vi.spyOn(target, 'getBoundingClientRect').mockImplementation(
      () => new DOMRect(0, targetTop, 100, 100),
    )

    act(flushAnimationFrames)
    expect(onLoadMore).not.toHaveBeenCalled()

    targetTop = 900
    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    expect(onLoadMore).toHaveBeenCalledTimes(1)
  })

  it('keeps IntersectionObserver as the browser prefetch path', async () => {
    const onLoadMore = vi.fn(async () => {})
    render(<Harness onLoadMore={onLoadMore} />)

    const observerCallback = intersectionObserverCallback
    expect(observerCallback).toBeDefined()

    await act(async () => {
      observerCallback?.(
        [{ isIntersecting: true } as IntersectionObserverEntry],
        intersectionObserverInstance!,
      )
    })

    const target = screen.getByTestId('infinite-scroll-target')
    expect(intersectionObserverOptions?.rootMargin).toBe('200px')
    expect(intersectionObserverInstance?.rootMargin).toBe('200px')
    expect(intersectionObserverInstance?.observe).toHaveBeenCalledTimes(1)
    expect(intersectionObserverInstance?.observe).toHaveBeenCalledWith(target)
    expect(onLoadMore).toHaveBeenCalledTimes(1)
  })

  it('starts only one load when observer and scroll fire together', async () => {
    let resolveLoad: (() => void) | undefined
    const pendingLoad = new Promise<void>((resolve) => {
      resolveLoad = resolve
    })
    const onLoadMore = vi.fn(() => pendingLoad)
    render(<Harness onLoadMore={onLoadMore} />)

    const target = screen.getByTestId('infinite-scroll-target')
    vi.spyOn(target, 'getBoundingClientRect').mockReturnValue(
      new DOMRect(0, 900, 100, 100),
    )

    await act(async () => {
      intersectionObserverCallback?.(
        [{ isIntersecting: true } as IntersectionObserverEntry],
        intersectionObserverInstance!,
      )
      fireEvent.scroll(window)
      flushAnimationFrames()
      await flushPromiseChain()
    })

    expect(onLoadMore).toHaveBeenCalledTimes(1)

    await act(async () => {
      resolveLoad?.()
      await pendingLoad
      await flushPromiseChain()
    })
  })

  it('releases the guard after failure so a later scroll can retry', async () => {
    const onLoadMore = vi.fn()
      .mockRejectedValueOnce(new Error('load failed'))
      .mockResolvedValueOnce(undefined)
    render(<Harness onLoadMore={onLoadMore} />)

    const target = screen.getByTestId('infinite-scroll-target')
    vi.spyOn(target, 'getBoundingClientRect').mockReturnValue(
      new DOMRect(0, 900, 100, 100),
    )

    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    expect(onLoadMore).toHaveBeenCalledTimes(2)
  })

  it('waits for a later event after failure when the callback identity changes', async () => {
    const rejectedOnLoadMore = vi.fn().mockRejectedValue(new Error('load failed'))
    const replacementOnLoadMore = vi.fn(async () => {})
    const { rerender } = render(<Harness onLoadMore={rejectedOnLoadMore} />)

    const target = screen.getByTestId('infinite-scroll-target')
    vi.spyOn(target, 'getBoundingClientRect').mockReturnValue(
      new DOMRect(0, 900, 100, 100),
    )

    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })
    expect(rejectedOnLoadMore).toHaveBeenCalledTimes(1)

    rerender(<Harness onLoadMore={replacementOnLoadMore} />)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    expect(replacementOnLoadMore).not.toHaveBeenCalled()

    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await flushPromiseChain()
    })

    expect(replacementOnLoadMore).toHaveBeenCalledTimes(1)
  })

  it('releases the guard after a synchronous throw so a later scroll can retry', async () => {
    let attempt = 0
    const onLoadMore = vi.fn(() => {
      attempt += 1
      if (attempt === 1) {
        throw new Error('load threw synchronously')
      }
      return Promise.resolve()
    })
    render(<Harness onLoadMore={onLoadMore} />)

    const target = screen.getByTestId('infinite-scroll-target')
    vi.spyOn(target, 'getBoundingClientRect').mockReturnValue(
      new DOMRect(0, 900, 100, 100),
    )

    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await waitForNextTask()
    })

    fireEvent.scroll(window)
    await act(async () => {
      flushAnimationFrames()
      await waitForNextTask()
    })

    expect(onLoadMore).toHaveBeenCalledTimes(2)
  })

  it('does not load when disabled', () => {
    const onLoadMore = vi.fn(async () => {})
    render(<Harness enabled={false} onLoadMore={onLoadMore} />)

    const target = screen.getByTestId('infinite-scroll-target')
    vi.spyOn(target, 'getBoundingClientRect').mockReturnValue(
      new DOMRect(0, 900, 100, 100),
    )

    fireEvent.scroll(window)
    act(flushAnimationFrames)

    expect(onLoadMore).not.toHaveBeenCalled()
  })

  it('disconnects observer and cancels queued work on unmount', () => {
    const onLoadMore = vi.fn(async () => {})
    const { unmount } = render(<Harness onLoadMore={onLoadMore} />)
    const observer = intersectionObserverInstance

    expect(animationFrames.size).toBe(1)
    unmount()
    fireEvent.scroll(window)

    expect(observer?.disconnect).toHaveBeenCalledTimes(1)
    expect(animationFrames.size).toBe(0)
    expect(onLoadMore).not.toHaveBeenCalled()
  })

  it('ignores a retained observer callback after the hook is disabled', async () => {
    const oldOnLoadMore = vi.fn(async () => {})
    const disabledOnLoadMore = vi.fn(async () => {})
    const { rerender } = render(<Harness onLoadMore={oldOnLoadMore} />)
    const staleObserverCallback = intersectionObserverCallback
    const staleObserver = intersectionObserverInstance

    rerender(<Harness enabled={false} onLoadMore={disabledOnLoadMore} />)

    await act(async () => {
      staleObserverCallback?.(
        [{ isIntersecting: true } as IntersectionObserverEntry],
        staleObserver!,
      )
      await waitForNextTask()
    })

    expect(oldOnLoadMore).not.toHaveBeenCalled()
    expect(disabledOnLoadMore).not.toHaveBeenCalled()
  })
})
