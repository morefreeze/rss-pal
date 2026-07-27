import { useRef } from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useInfiniteScrollTrigger } from '../src/hooks/useInfiniteScrollTrigger'

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
})
