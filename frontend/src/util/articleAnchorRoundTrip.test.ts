import { fireEvent } from '@testing-library/dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { clearArticleAnchorRoundTrip, startArticleAnchorRoundTrip } from './articleAnchorRoundTrip'

function addAnchor(id: string): HTMLAnchorElement {
  const anchor = document.createElement('a')
  anchor.id = id
  anchor.href = '#article-section-001'
  anchor.scrollIntoView = vi.fn()
  document.body.append(anchor)
  return anchor
}

function addTarget(): HTMLElement {
  const target = document.createElement('p')
  target.id = 'article-section-001'
  vi.spyOn(target, 'getBoundingClientRect').mockReturnValue({
    x: 20,
    y: 40,
    left: 20,
    top: 40,
    right: 220,
    bottom: 80,
    width: 200,
    height: 40,
    toJSON: () => ({}),
  })
  document.body.append(target)
  return target
}

function setReducedMotion(matches: boolean) {
  vi.stubGlobal('matchMedia', vi.fn(() => ({
    matches,
    media: '(prefers-reduced-motion: reduce)',
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })))
}

describe('article anchor round trip', () => {
  beforeEach(() => {
    history.replaceState(null, '', '/articles/1')
    setReducedMotion(false)
  })

  afterEach(() => {
    clearArticleAnchorRoundTrip()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    document.body.replaceChildren()
  })

  it('creates one semantic return anchor and returns without changing history', () => {
    const source = addAnchor('summary-article-source-1')
    const target = addTarget()
    const historyLength = history.length
    const onReturn = vi.fn()

    startArticleAnchorRoundTrip(source, target, { onReturn })

    const back = document.querySelector<HTMLAnchorElement>('.article-anchor-return-link')
    expect(back?.textContent).toBe('↩⌖')
    expect(back?.getAttribute('href')).toBe('#summary-article-source-1')
    expect(back?.getAttribute('aria-label')).toBe('跳回 AI 总结')
    expect(back?.getAttribute('title')).toBe('跳回 AI 总结')
    expect(back?.parentElement).toBe(target)

    fireEvent.click(back!, { detail: 1 })

    expect(source.scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' })
    expect(onReturn).toHaveBeenCalledTimes(1)
    expect(document.querySelector('.article-anchor-return-link')).toBeNull()
    expect(location.hash).toBe('')
    expect(history.length).toBe(historyLength)
  })

  it('keeps the return link in the paragraph flow without measuring on scroll', () => {
    const source = addAnchor('summary-article-source-1')
    const target = addTarget()
    target.textContent = 'A wrapped destination sentence'
    startArticleAnchorRoundTrip(source, target)
    const back = target.querySelector<HTMLAnchorElement>('.article-anchor-return-link')!
    expect(back).not.toBeNull()
    expect(target.lastChild).toBe(back)
    expect(back.style.left).toBe('')
    expect(back.style.top).toBe('')
    vi.mocked(target.getBoundingClientRect).mockClear()
    window.dispatchEvent(new Event('scroll'))
    expect(target.getBoundingClientRect).not.toHaveBeenCalled()
  })

  it.each(['table', 'div', 'pre'])('places the return link after a %s block', (tag) => {
    const source = addAnchor('summary-article-source-1')
    const article = document.createElement('article')
    const target = document.createElement(tag)
    article.append(target)
    document.body.append(article)
    startArticleAnchorRoundTrip(source, target)
    const back = article.querySelector('.article-anchor-return-link')!
    expect(back).not.toBeNull()
    expect(target.contains(back)).toBe(false)
    expect(target.nextElementSibling?.contains(back)).toBe(true)
    clearArticleAnchorRoundTrip()
    expect(article.children).toHaveLength(1)
  })

  it('appends inside the final paragraph of a quote', () => {
    const source = addAnchor('summary-article-source-1')
    const target = document.createElement('blockquote')
    target.innerHTML = '<p>First</p><p>Last <a href="https://example.com">link</a></p>'
    document.body.append(target)
    startArticleAnchorRoundTrip(source, target)
    const back = target.querySelector('.article-anchor-return-link')!
    expect(back.parentElement).toBe(target.lastElementChild)
    expect(back.closest('a[href="https://example.com"]')).toBeNull()
  })

  it('uses instant scrolling and focuses the source for keyboard activation', () => {
    const source = addAnchor('summary-article-source-1')
    const target = addTarget()
    setReducedMotion(true)
    const focus = vi.spyOn(source, 'focus')

    startArticleAnchorRoundTrip(source, target)
    fireEvent.click(document.querySelector('.article-anchor-return-link')!, { detail: 0 })

    expect(source.scrollIntoView).toHaveBeenCalledWith({ behavior: 'auto', block: 'center' })
    expect(focus).toHaveBeenCalledWith({ preventScroll: true })
  })

  it('keeps modified and auxiliary return activations inert', () => {
    const source = addAnchor('summary-article-source-1')
    const target = addTarget()
    startArticleAnchorRoundTrip(source, target)
    const back = document.querySelector<HTMLAnchorElement>('.article-anchor-return-link')!

    expect(fireEvent.click(back, { ctrlKey: true, detail: 1 })).toBe(false)
    const auxClick = new MouseEvent('auxclick', { bubbles: true, cancelable: true, button: 1 })
    expect(back.dispatchEvent(auxClick)).toBe(false)
    expect(source.scrollIntoView).not.toHaveBeenCalled()
    expect(document.querySelector('.article-anchor-return-link')).toBe(back)
    expect(location.hash).toBe('')
  })

  it('replaces an older trip and only lets the owning source clear it', () => {
    const first = addAnchor('summary-article-source-1')
    const second = addAnchor('summary-article-source-2')
    const firstTarget = addTarget()
    const secondTarget = addTarget()

    startArticleAnchorRoundTrip(first, firstTarget)
    startArticleAnchorRoundTrip(second, secondTarget)

    expect(document.querySelectorAll('.article-anchor-return-link')).toHaveLength(1)
    expect(document.querySelector('.article-anchor-return-link')?.getAttribute('href'))
      .toBe('#summary-article-source-2')

    clearArticleAnchorRoundTrip(first)
    expect(document.querySelector('.article-anchor-return-link')).not.toBeNull()

    clearArticleAnchorRoundTrip(second)
    expect(document.querySelector('.article-anchor-return-link')).toBeNull()
  })

  it('cleans up safely when the recorded source is detached', () => {
    const source = addAnchor('summary-article-source-1')
    const target = addTarget()
    const onReturn = vi.fn()
    startArticleAnchorRoundTrip(source, target, { onReturn })
    source.remove()

    fireEvent.click(document.querySelector('.article-anchor-return-link')!, { detail: 1 })

    expect(source.scrollIntoView).not.toHaveBeenCalled()
    expect(onReturn).not.toHaveBeenCalled()
    expect(document.querySelector('.article-anchor-return-link')).toBeNull()
    expect(location.hash).toBe('')
  })

  it('cleans up when the target detaches without waiting for scrolling', async () => {
    const source = addAnchor('summary-article-source-1')
    const target = addTarget()
    startArticleAnchorRoundTrip(source, target)
    target.remove()

    await new Promise(resolve => setTimeout(resolve, 0))

    expect(document.querySelector('.article-anchor-return-link')).toBeNull()
  })
})
