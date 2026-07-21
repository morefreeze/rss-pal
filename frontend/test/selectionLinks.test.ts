import { afterEach, describe, expect, it } from 'vitest'
import { resolveSelectionLinks } from '../src/reader/selectionLinks'

function mount(html: string): HTMLElement {
  const root = document.createElement('article')
  root.innerHTML = html
  document.body.append(root)
  return root
}

function select(start: Node, startOffset: number, end: Node, endOffset: number): Selection {
  const range = document.createRange()
  range.setStart(start, startOffset)
  range.setEnd(end, endOffset)
  const selection = window.getSelection()
  if (!selection) throw new Error('Selection is unavailable')
  selection.removeAllRanges()
  selection.addRange(range)
  return selection
}

function urls(root: HTMLElement, selection: Selection): string[] {
  return resolveSelectionLinks(root, selection, (href) => {
    try {
      const url = new URL(href, 'https://reader.example/article')
      return /^https?:$/.test(url.protocol) ? url.href : null
    } catch {
      return null
    }
  }).map((link) => link.url)
}

afterEach(() => {
  window.getSelection()?.removeAllRanges()
  document.body.replaceChildren()
})

describe('resolveSelectionLinks', () => {
  it('promotes a partial selection to the complete link target', () => {
    const root = mount('<p>Before <a href="/alpha">Alpha readable title</a> after</p>')
    const anchor = root.querySelector('a')!
    const text = anchor.firstChild!

    const links = resolveSelectionLinks(root, select(text, 6, text, 14), (href) => new URL(href, 'https://reader.example').href)

    expect(links).toHaveLength(1)
    expect(links[0]).toMatchObject({
      url: 'https://reader.example/alpha',
      title: 'Alpha readable title',
      element: anchor,
    })
  })

  it('includes a link when selection enters or leaves it', () => {
    const root = mount('<p><span>Before </span><a href="/alpha">Alpha</a><span> after</span></p>')
    const [before, anchor, after] = root.querySelector('p')!.children

    expect(urls(root, select(before.firstChild!, 2, anchor.firstChild!, 2))).toEqual([
      'https://reader.example/alpha',
    ])
    expect(urls(root, select(anchor.firstChild!, 2, after.firstChild!, 3))).toEqual([
      'https://reader.example/alpha',
    ])
  })

  it('returns every intersected link in document order', () => {
    const root = mount('<p><span>Start </span><a href="/a">First</a> middle <a href="/b">Second</a><span> end</span></p>')
    const before = root.querySelector('span')!
    const after = root.querySelectorAll('span')[1]

    expect(urls(root, select(before.firstChild!, 1, after.firstChild!, 2))).toEqual([
      'https://reader.example/a',
      'https://reader.example/b',
    ])
  })

  it('does not count a link touched only at an exact selection boundary', () => {
    const root = mount('<p><span>Plain</span><a href="/a">Link</a><span>tail</span></p>')
    const plain = root.querySelector('span')!
    const anchor = root.querySelector('a')!

    expect(urls(root, select(plain.firstChild!, 0, anchor.firstChild!, 0))).toEqual([])
  })

  it('does nothing for pure text selections', () => {
    const root = mount('<p><span>Only plain text</span> <a href="/a">Link</a></p>')
    const text = root.querySelector('span')!.firstChild!
    expect(urls(root, select(text, 0, text, 4))).toEqual([])
  })

  it('skips links inside code and pre blocks', () => {
    const root = mount('<p><a href="/ok">Keep</a></p><code><a href="/code">Code</a></code><pre><a href="/pre">Pre</a></pre>')
    const range = document.createRange()
    range.selectNodeContents(root)
    const selection = window.getSelection()!
    selection.removeAllRanges()
    selection.addRange(range)

    expect(urls(root, selection)).toEqual(['https://reader.example/ok'])
  })

  it('rejects selections outside the reader root and invalid schemes', () => {
    const root = mount('<p><a href="mailto:test@example.com">Mail</a></p>')
    const outside = mount('<p>Outside</p>')
    const outsideText = outside.querySelector('p')!.firstChild!
    expect(urls(root, select(outsideText, 0, outsideText, 3))).toEqual([])

    const mailText = root.querySelector('a')!.firstChild!
    expect(urls(root, select(mailText, 0, mailText, 2))).toEqual([])
  })

  it('deduplicates normalized URLs and keeps the first anchor', () => {
    const root = mount('<p><a href="https://example.com/a">First</a> <a href="https://example.com/a">Second</a></p>')
    const range = document.createRange()
    range.selectNodeContents(root)
    const selection = window.getSelection()!
    selection.removeAllRanges()
    selection.addRange(range)

    const links = resolveSelectionLinks(root, selection, (href) => href)
    expect(links).toHaveLength(1)
    expect(links[0].title).toBe('First')
    expect(links[0].element).toBe(root.querySelector('a'))
  })
})
