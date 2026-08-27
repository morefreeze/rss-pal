import { act, fireEvent, render, screen } from '@testing-library/react'
import ReactMarkdown from 'react-markdown'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import SummaryMarkdown from './SummaryMarkdown'
import { clearArticleAnchorRoundTrip } from '../util/articleAnchorRoundTrip'

const HIGHLIGHT_TIMEOUT_MS = 7_100

function addArticleTarget(id = 'article-section-001'): HTMLElement {
  const target = document.createElement('h2')
  target.id = id
  target.textContent = 'Target section'
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

function linkAttributes(link: HTMLAnchorElement) {
  return [...link.attributes]
    .map(({ name, value }) => [name, value])
    .sort(([left], [right]) => left.localeCompare(right))
}

describe('SummaryMarkdown article links', () => {
  let scrollIntoView: ReturnType<typeof vi.fn>

  beforeEach(() => {
    scrollIntoView = vi.fn()
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
      configurable: true,
      value: scrollIntoView,
    })
    setReducedMotion(false)
  })

  afterEach(() => {
    clearArticleAnchorRoundTrip()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    document.body.replaceChildren()
  })

  it.each(['查看原文', '跳转原文', '任意文案'])(
    'renders the stored article reference %s as one icon-only semantic anchor',
    (storedLabel) => {
      render(<SummaryMarkdown source={`[${storedLabel}](#article-section-001)`} />)

      const articleLink = screen.getByRole('link', { name: '跳转原文' })
      const articleIcons = articleLink.querySelectorAll('.summary-article-link-icon')
      expect(articleLink.textContent).toBe('⌖')
      expect(articleLink.id).toMatch(/^summary-article-source-/)
      expect(articleLink.getAttribute('href')).toBe('#article-section-001')
      expect(articleLink.getAttribute('title')).toBe('跳转原文')
      expect(articleLink.classList.contains('summary-article-link')).toBe(true)
      expect(articleIcons).toHaveLength(1)
      expect(articleIcons[0].getAttribute('aria-hidden')).toBe('true')
    },
  )

  it.each([
    ['External', 'https://example.com'],
    ['Fragment', '#other-section'],
    ['Invalid', '#article-section-01'],
  ])('does not normalize the non-article link %s', (label, href) => {
    render(<SummaryMarkdown source={`[${label}](${href})`} />)

    const link = screen.getByRole('link', { name: label })
    expect(link.textContent).toBe(label)
    expect(link.getAttribute('href')).toBe(href)
    expect(link.classList.contains('summary-article-link')).toBe(false)
    expect(link.querySelector('.summary-article-link-icon')).toBeNull()
  })

  it('scrolls a valid body target, highlights it, and cleans up after its animation', () => {
    const target = addArticleTarget()
    render(<SummaryMarkdown source="[Jump](#article-section-001)" />)

    expect(fireEvent.click(screen.getByRole('link', { name: '跳转原文' }), { detail: 1 })).toBe(false)
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' })
    expect(target.classList.contains('article-anchor-highlight')).toBe(true)

    fireEvent.animationEnd(target)
    expect(target.classList.contains('article-anchor-highlight')).toBe(false)
  })

  it('creates a semantic return anchor and completes the round trip without changing history', () => {
    history.replaceState(null, '', '/articles/1')
    const target = addArticleTarget()
    const historyLength = history.length
    render(<SummaryMarkdown source="[Jump](#article-section-001)" />)
    const source = screen.getByRole('link', { name: '跳转原文' })

    fireEvent.click(source, { detail: 1 })

    const back = screen.getByRole('link', { name: '跳回 AI 总结' })
    expect(back.textContent).toBe('↩⌖')
    expect(back.getAttribute('href')).toBe(`#${source.id}`)
    expect(location.hash).toBe('')
    expect(history.length).toBe(historyLength)
    expect(scrollIntoView).toHaveBeenNthCalledWith(1, { behavior: 'smooth', block: 'center' })

    fireEvent.click(back, { detail: 1 })

    expect(scrollIntoView).toHaveBeenNthCalledWith(2, { behavior: 'smooth', block: 'center' })
    expect(document.querySelector('.article-anchor-return-link')).toBeNull()
    expect(target.classList.contains('article-anchor-highlight')).toBe(true)
    expect(location.hash).toBe('')
    expect(history.length).toBe(historyLength)
  })

  it('moves the single return anchor to the latest summary source', () => {
    addArticleTarget('article-section-001')
    addArticleTarget('article-section-002')
    render(<SummaryMarkdown source={'[First](#article-section-001)\n\n[Second](#article-section-002)'} />)
    const [first, second] = screen.getAllByRole('link', { name: '跳转原文' })

    fireEvent.click(first, { detail: 1 })
    fireEvent.click(second, { detail: 1 })

    expect(document.querySelectorAll('.article-anchor-return-link')).toHaveLength(1)
    expect(screen.getByRole('link', { name: '跳回 AI 总结' }).getAttribute('href'))
      .toBe(`#${second.id}`)
  })

  it('removes the return anchor when its summary source unmounts', () => {
    addArticleTarget()
    const view = render(<SummaryMarkdown source="[Jump](#article-section-001)" />)
    fireEvent.click(screen.getByRole('link', { name: '跳转原文' }), { detail: 1 })
    expect(screen.getByRole('link', { name: '跳回 AI 总结' })).toBeTruthy()

    view.unmount()

    expect(document.querySelector('.article-anchor-return-link')).toBeNull()
  })

  it('safely consumes a valid href whose body target is absent', () => {
    render(<SummaryMarkdown source="[Missing](#article-section-001)" />)

    expect(fireEvent.click(screen.getByRole('link', { name: '跳转原文' }), { detail: 1 })).toBe(false)
    expect(scrollIntoView).not.toHaveBeenCalled()
    expect(document.querySelector('.article-anchor-return-link')).toBeNull()
  })

  it.each([
    ['Ctrl-click', 'click', { button: 0, ctrlKey: true }, true],
    ['Cmd-click', 'click', { button: 0, metaKey: true }, true],
    ['Shift-click', 'click', { button: 0, shiftKey: true }, true],
    ['Alt-click', 'click', { button: 0, altKey: true }, true],
    ['middle click', 'click', { button: 1 }, false],
    ['middle auxclick', 'auxclick', { button: 1 }, false],
    ['right auxclick', 'auxclick', { button: 2 }, false],
  ])('keeps a strict article anchor inert for %s', (_name, eventType, init, withTarget) => {
    const target = withTarget ? addArticleTarget() : null
    render(<SummaryMarkdown source="[Jump](#article-section-001)" />)
    const link = screen.getByRole('link', { name: '跳转原文' })
    const activation = new MouseEvent(eventType, {
      bubbles: true,
      cancelable: true,
      ...init,
    })

    expect(link.dispatchEvent(activation)).toBe(false)
    expect(activation.defaultPrevented).toBe(true)
    expect(link.getAttribute('href')).toBe('#article-section-001')
    expect(scrollIntoView).not.toHaveBeenCalled()
    expect(target?.classList.contains('article-anchor-highlight') ?? false).toBe(false)
  })

  it('uses instant scrolling when reduced motion is preferred', () => {
    vi.useFakeTimers()
    const target = addArticleTarget()
    setReducedMotion(true)
    render(<SummaryMarkdown source="[Jump](#article-section-001)" />)

    fireEvent.click(screen.getByRole('link', { name: '跳转原文' }), { detail: 1 })
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: 'auto', block: 'center' })
    expect(target.classList.contains('article-anchor-highlight')).toBe(true)
    act(() => vi.advanceTimersByTime(7_000))
    expect(target.classList.contains('article-anchor-highlight')).toBe(true)
    act(() => vi.advanceTimersByTime(100))
    expect(target.classList.contains('article-anchor-highlight')).toBe(false)
  })

  it('keeps the highlight until the 7100ms fallback boundary', () => {
    vi.useFakeTimers()
    const target = addArticleTarget()
    render(<SummaryMarkdown source="[Jump](#article-section-001)" />)

    fireEvent.click(screen.getByRole('link', { name: '跳转原文' }), { detail: 1 })
    act(() => vi.advanceTimersByTime(6_999))
    expect(target.classList.contains('article-anchor-highlight')).toBe(true)
    act(() => vi.advanceTimersByTime(101))
    expect(target.classList.contains('article-anchor-highlight')).toBe(false)
  })

  it('does not steal focus for mouse activation but temporarily focuses a keyboard target', () => {
    const target = addArticleTarget()
    const before = document.createElement('button')
    document.body.append(before)
    before.focus()
    render(<SummaryMarkdown source="[Jump](#article-section-001)" />)
    const link = screen.getByRole('link', { name: '跳转原文' })

    fireEvent.click(link, { detail: 1 })
    expect(document.activeElement).toBe(before)
    expect(target.hasAttribute('tabindex')).toBe(false)

    fireEvent.click(link, { detail: 0 })
    expect(document.activeElement).toBe(target)
    expect(target.hasAttribute('tabindex')).toBe(false)
  })

  it('does not let an older highlight timeout remove a newer highlight', () => {
    vi.useFakeTimers()
    const target = addArticleTarget()
    render(<SummaryMarkdown source="[Jump](#article-section-001)" />)
    const link = screen.getByRole('link', { name: '跳转原文' })

    fireEvent.click(link, { detail: 1 })
    act(() => vi.advanceTimersByTime(500))
    fireEvent.click(link, { detail: 1 })
    act(() => vi.advanceTimersByTime(HIGHLIGHT_TIMEOUT_MS - 500))
    expect(target.classList.contains('article-anchor-highlight')).toBe(true)
    act(() => vi.advanceTimersByTime(500))
    expect(target.classList.contains('article-anchor-highlight')).toBe(false)
  })

  it('preserves the baseline rendering and default click behavior for non-article links', () => {
    vi.useFakeTimers()
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    const source = [
      '[External](https://example.com)',
      '[Fragment](#other-section)',
      '[Malformed](#article-section-000)',
      '[Short](#article-section-01)',
      '[Suffix](#article-section-001x)',
    ].join('\n\n')
    const baseline = render(<ReactMarkdown>{source}</ReactMarkdown>)
    const baselineAttributes = new Map(
      (screen.getAllByRole('link') as HTMLAnchorElement[]).map((link) => [link.textContent, linkAttributes(link)]),
    )
    baseline.unmount()

    render(<SummaryMarkdown source={source} />)
    for (const link of screen.getAllByRole('link') as HTMLAnchorElement[]) {
      expect(linkAttributes(link)).toEqual(baselineAttributes.get(link.textContent))
      expect(fireEvent.click(link, { detail: 1 })).toBe(true)
    }
    expect(scrollIntoView).not.toHaveBeenCalled()
    expect(consoleError).not.toHaveBeenCalled()
  })
})
