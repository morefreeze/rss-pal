import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ExploreArticleCard from '../src/components/ExploreArticleCard'
import type { ExploreArticleListItem } from '../src/api/client'

class FakeIntersectionObserver {
  static instances: FakeIntersectionObserver[] = []
  readonly callback: IntersectionObserverCallback
  disconnect = vi.fn()
  observe = vi.fn()
  unobserve = vi.fn()
  takeRecords = vi.fn(() => [])
  root = null
  rootMargin = '0px'
  thresholds = [0, 0.5, 1]

  constructor(callback: IntersectionObserverCallback) {
    this.callback = callback
    FakeIntersectionObserver.instances.push(this)
  }

  intersect(isIntersecting: boolean, intersectionRatio: number) {
    this.callback([{ isIntersecting, intersectionRatio } as IntersectionObserverEntry], this as unknown as IntersectionObserver)
  }
}

const article: ExploreArticleListItem = {
  id: 7,
  source_id: 3,
  source_title: 'Source 3',
  title: 'Retryable exposure',
  url: 'https://example.test/7',
  excerpt: '',
  published_at: '2026-08-31T08:00:00Z',
  fetched_at: '2026-08-31T09:00:00Z',
  topic: '工程',
  reason: '测试',
  is_subscribed: false,
}

describe('ExploreArticleCard exposure tracking', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    FakeIntersectionObserver.instances = []
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('keeps observing after a failed exposure and disconnects only after a later success', async () => {
    const onExposure = vi.fn()
      .mockResolvedValueOnce(false)
      .mockResolvedValueOnce(true)
    const view = render(
      <ExploreArticleCard
        article={article}
        sort="published"
        onOpen={vi.fn()}
        onExposure={onExposure}
        onHideSource={vi.fn()}
        onDampenTopic={vi.fn()}
      />,
    )
    const observer = FakeIntersectionObserver.instances[0]

    act(() => observer.intersect(true, 0.5))
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(onExposure).toHaveBeenCalledTimes(1)
    expect(observer.disconnect).not.toHaveBeenCalled()

    act(() => observer.intersect(false, 0))
    act(() => observer.intersect(true, 0.5))
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(onExposure).toHaveBeenCalledTimes(2)
    expect(observer.disconnect).toHaveBeenCalledTimes(1)

    view.unmount()
    expect(observer.disconnect).toHaveBeenCalledTimes(2)
  })

  it.each(['Enter', ' '])('exposes one native title action for %j without making the article interactive', async key => {
    vi.useRealTimers()
    const user = userEvent.setup()
    const onOpen = vi.fn()
    const view = render(
      <ExploreArticleCard
        article={article}
        sort="published"
        onOpen={onOpen}
        onExposure={vi.fn().mockResolvedValue(true)}
        onHideSource={vi.fn()}
        onDampenTopic={vi.fn()}
      />,
    )
    const card = screen.getByRole('article', { name: article.title })
    const titleAction = screen.getByRole('button', { name: article.title })

    expect(card.getAttribute('tabindex')).toBeNull()
    titleAction.focus()
    await user.keyboard(key === 'Enter' ? '{Enter}' : ' ')

    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(onOpen).toHaveBeenCalledWith(article)
    expect(view.container.querySelector('article button button')).toBeNull()
  })

  it('keeps the menu action independent from the title navigation action', () => {
    const onOpen = vi.fn()
    render(
      <ExploreArticleCard
        article={article}
        sort="published"
        onOpen={onOpen}
        onExposure={vi.fn().mockResolvedValue(true)}
        onHideSource={vi.fn()}
        onDampenTopic={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: `${article.title} 的更多操作` }))
    expect(screen.getByRole('menu')).toBeTruthy()
    expect(onOpen).not.toHaveBeenCalled()
  })
})
