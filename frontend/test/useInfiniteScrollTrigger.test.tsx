import { useRef } from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useInfiniteScrollTrigger } from '../src/hooks/useInfiniteScrollTrigger'

let intersectionObserverCallback: IntersectionObserverCallback | undefined
let intersectionObserverInstance: TestIntersectionObserver | undefined

class TestIntersectionObserver implements IntersectionObserver {
  readonly root = null
  readonly rootMargin = '0px'
  readonly thresholds = [0]
  observe: IntersectionObserver['observe'] = vi.fn()
  unobserve: IntersectionObserver['unobserve'] = vi.fn()
  disconnect: IntersectionObserver['disconnect'] = vi.fn()
  takeRecords: IntersectionObserver['takeRecords'] = vi.fn(() => [])

  constructor(callback: IntersectionObserverCallback) {
    intersectionObserverCallback = callback
    intersectionObserverInstance = this
  }
}

function Harness({ onLoadMore }: { onLoadMore: () => Promise<void> }) {
  const targetRef = useRef<HTMLDivElement>(null)

  useInfiniteScrollTrigger({
    targetRef,
    enabled: true,
    refreshKey: 0,
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

  it('loads when scrolling brings the target within the viewport margin', () => {
    let targetTop = 10_000
    const onLoadMore = vi.fn(async () => {})
    render(<Harness onLoadMore={onLoadMore} />)

    const target = screen.getByTestId('infinite-scroll-target')
    vi.spyOn(target, 'getBoundingClientRect').mockImplementation(
      () => new DOMRect(0, targetTop, 100, 100),
    )

    const flushAnimationFrames = () => {
      const queuedFrames = Array.from(animationFrames.values())
      animationFrames.clear()
      queuedFrames.forEach((callback) => callback(performance.now()))
    }

    act(flushAnimationFrames)
    expect(onLoadMore).not.toHaveBeenCalled()

    targetTop = 900
    fireEvent.scroll(window)
    act(flushAnimationFrames)

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

    expect(intersectionObserverInstance?.observe).toHaveBeenCalledTimes(1)
    expect(onLoadMore).toHaveBeenCalledTimes(1)
  })
})
