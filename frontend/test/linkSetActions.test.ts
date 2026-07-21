import { describe, expect, it, vi } from 'vitest'
import { createLinkSetActions } from '../src/reader/linkSetActions'
import type { ReaderContextTarget, ReaderLinkTarget } from '../src/reader/types'

function link(url: string, title: string): ReaderLinkTarget {
  const element = document.createElement('a')
  element.href = url
  element.textContent = title
  return { url, title, element }
}

function target(
  links: ReaderLinkTarget[],
  kind: ReaderContextTarget['kind'] = 'selection-links',
): ReaderContextTarget {
  if (kind === 'long-press-link') {
    return { kind, links: [links[0]], anchorRect: new DOMRect() }
  }
  return { kind, links, anchorRect: new DOMRect() }
}

function callbacks() {
  return {
    onAdd: vi.fn(),
    onRemove: vi.fn(),
    onOpen: vi.fn(),
    onCopy: vi.fn(),
  }
}

describe('createLinkSetActions', () => {
  it('adds unmarked selection links in target order', async () => {
    const events = callbacks()
    const links = [link('https://a.example/', 'A'), link('https://b.example/', 'B')]
    const actions = createLinkSetActions({
      target: target(links),
      draftURLs: new Set(),
      fetchedURLs: new Set(),
      ...events,
    })

    expect(actions.map((action) => action.label)).toEqual(['加入待抓取（2）'])
    await actions[0].run()
    expect(events.onAdd).toHaveBeenCalledWith([
      { url: 'https://a.example/', title: 'A' },
      { url: 'https://b.example/', title: 'B' },
    ])
  })

  it('removes marked links', async () => {
    const events = callbacks()
    const links = [link('https://a.example/', 'A'), link('https://b.example/', 'B')]
    const actions = createLinkSetActions({
      target: target(links),
      draftURLs: new Set(links.map((item) => item.url)),
      fetchedURLs: new Set(),
      ...events,
    })
    expect(actions.map((action) => action.label)).toEqual(['移出待抓取（2）'])
    await actions[0].run()
    expect(events.onRemove).toHaveBeenCalledWith(['https://a.example/', 'https://b.example/'])
  })

  it('separates mixed unmarked, draft, and fetched targets', async () => {
    const events = callbacks()
    const links = [
      link('https://a.example/', 'A'),
      link('https://b.example/', 'B'),
      link('https://c.example/', 'C'),
    ]
    const actions = createLinkSetActions({
      target: target(links),
      draftURLs: new Set([links[1].url]),
      fetchedURLs: new Set([links[2].url]),
      ...events,
    })
    expect(actions.map((action) => action.label)).toEqual([
      '加入待抓取（1）',
      '移出待抓取（1）',
      '已抓取（1）',
    ])
    expect(actions[2].disabled).toBe(true)
    await actions[0].run()
    await actions[1].run()
    expect(events.onAdd).toHaveBeenCalledWith([{ url: links[0].url, title: 'A' }])
    expect(events.onRemove).toHaveBeenCalledWith([links[1].url])
  })

  it('renders a fetched-only selection as readonly', () => {
    const events = callbacks()
    const item = link('https://a.example/', 'A')
    const actions = createLinkSetActions({
      target: target([item]),
      draftURLs: new Set([item.url]),
      fetchedURLs: new Set([item.url]),
      ...events,
    })
    expect(actions.map((action) => [action.label, action.disabled])).toEqual([
      ['已抓取（1）', true],
    ])
  })

  it('adds mobile open and copy actions without counts', async () => {
    const events = callbacks()
    const item = link('https://a.example/', 'A')
    const actions = createLinkSetActions({
      target: target([item], 'long-press-link'),
      draftURLs: new Set(),
      fetchedURLs: new Set(),
      ...events,
    })
    expect(actions.map((action) => action.label)).toEqual([
      '加入待抓取',
      '在新标签页打开',
      '复制链接',
    ])
    await actions[1].run()
    await actions[2].run()
    expect(events.onOpen).toHaveBeenCalledWith(item.url)
    expect(events.onCopy).toHaveBeenCalledWith(item.url)
  })

  it('uses remove or readonly state for a long-pressed draft or fetched link', () => {
    const events = callbacks()
    const item = link('https://a.example/', 'A')
    const base = { target: target([item], 'long-press-link'), fetchedURLs: new Set<string>(), ...events }
    expect(createLinkSetActions({ ...base, draftURLs: new Set([item.url]) })[0].label).toBe('移出待抓取')
    const fetched = createLinkSetActions({
      ...base,
      draftURLs: new Set(),
      fetchedURLs: new Set([item.url]),
    })
    expect([fetched[0].label, fetched[0].disabled]).toEqual(['已抓取', true])
  })
})
