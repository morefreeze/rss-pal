import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ArticleReadSurface from '../src/components/ArticleReadSurface'

afterEach(() => vi.useRealTimers())
function setup() {
  const mark = vi.fn(),
    open = vi.fn()
  render(
    <ArticleReadSurface onMarkRead={mark}>
      <div data-testid="card" onClick={open}>
        文章<button>来源</button><a href="https://example.com">原文链接</a>
      </div>
    </ArticleReadSurface>,
  )
  return { card: screen.getByTestId('card'), mark, open }
}
function start(el: Element, x = 150, y = 80) {
  fireEvent.touchStart(el, { touches: [{ clientX: x, clientY: y }] })
}
function move(el: Element, x: number, y = 80) {
  fireEvent.touchMove(el, { touches: [{ clientX: x, clientY: y }] })
}
function end(el: Element, x: number, y = 80) {
  fireEvent.touchEnd(el, {
    touches: [],
    changedTouches: [{ clientX: x, clientY: y }],
  })
}
describe('article read gestures', () => {
  it('left swipe reads only this article and suppresses navigation', () => {
    const { card, mark, open } = setup()
    start(card)
    move(card, 40)
    end(card, 40)
    fireEvent.click(card)
    expect(mark).toHaveBeenCalledWith('single')
    expect(open).not.toHaveBeenCalled()
  })
  it('right swipe opens inclusive range choices without reading yet', () => {
    const { card, mark } = setup()
    start(card, 40)
    move(card, 160)
    end(card, 160)
    expect(mark).not.toHaveBeenCalled()
    fireEvent.click(
      screen.getByRole('menuitem', { name: '这篇及以下标记已读' }),
    )
    expect(mark).toHaveBeenCalledWith('below')
  })
  it('long press reads this article once', () => {
    vi.useFakeTimers()
    const { card, mark, open } = setup()
    start(card)
    act(() => vi.advanceTimersByTime(550))
    end(card, 150)
    fireEvent.click(card)
    expect(mark.mock.calls).toEqual([['single']])
    expect(open).not.toHaveBeenCalled()
  })
  it('suppresses release after a long hold exceeding the click suppression timeout', () => {
    vi.useFakeTimers()
    const { card, mark, open } = setup()
    start(card)
    act(() => vi.advanceTimersByTime(2400))
    end(card, 150)
    fireEvent.click(card)
    expect(mark.mock.calls).toEqual([['single']])
    expect(open).not.toHaveBeenCalled()
  })
  it('vertical scroll cannot turn into a read swipe or long press', () => {
    vi.useFakeTimers()
    const { card, mark } = setup()
    start(card)
    move(card, 155, 110)
    move(card, 10, 130)
    act(() => vi.advanceTimersByTime(600))
    end(card, 10, 130)
    expect(mark).not.toHaveBeenCalled()
  })
  it('touch cancel and multi-touch cancel the action', () => {
    vi.useFakeTimers()
    const { card, mark } = setup()
    start(card)
    fireEvent.touchCancel(card)
    act(() => vi.advanceTimersByTime(600))
    start(card)
    fireEvent.touchMove(card, {
      touches: [
        { clientX: 5, clientY: 80 },
        { clientX: 10, clientY: 80 },
      ],
    })
    end(card, 5)
    expect(mark).not.toHaveBeenCalled()
  })
  it('leaves interactive descendants alone', () => {
    vi.useFakeTimers()
    const { mark } = setup()
    const button = screen.getByText('来源')
    start(button)
    act(() => vi.advanceTimersByTime(600))
    end(button, 10)
    expect(mark).not.toHaveBeenCalled()
  })
  it('suppresses a delayed native context menu even after touch cancellation', () => {
    const {card} = setup()
    start(card)
    fireEvent.touchCancel(card)
    expect(fireEvent.contextMenu(card)).toBe(false)
  })
  it('keeps native menus for links and controls even during a card touch gesture', () => {
    const {card} = setup()
    start(card)
    expect(fireEvent.contextMenu(screen.getByRole('link',{name:'原文链接'}))).toBe(true)
    expect(fireEvent.contextMenu(screen.getByText('来源'))).toBe(true)
  })
  it('preserves desktop right-click and keyboard context menus', () => {
    const {card} = setup()
    expect(fireEvent.contextMenu(card)).toBe(true)
    start(card)
    fireEvent.touchCancel(card)
    fireEvent.keyDown(card,{key:'ContextMenu'})
    expect(fireEvent.contextMenu(card)).toBe(true)
    start(card)
    fireEvent.touchCancel(card)
    const mouse = new Event('pointerdown',{bubbles:true})
    Object.defineProperty(mouse,'pointerType',{value:'mouse'})
    fireEvent(card,mouse)
    expect(fireEvent.contextMenu(card)).toBe(true)
  })
  it('prevents the native menu before a child can stop event propagation', () => {
    const mark=vi.fn()
    render(<ArticleReadSurface onMarkRead={mark}><div data-testid="nested" onContextMenu={e=>e.stopPropagation()}>摘要文字</div></ArticleReadSurface>)
    const text=screen.getByTestId('nested')
    start(text)
    expect(fireEvent.contextMenu(text)).toBe(false)
  })
  it('desktop menu offers single and inclusive ranges', () => {
    const { mark } = setup()
    fireEvent.click(screen.getByRole('button', { name: '文章已读操作' }))
    fireEvent.click(
      screen.getByRole('menuitem', { name: '这篇及以上标记已读' }),
    )
    expect(mark).toHaveBeenCalledWith('above')
  })
})
