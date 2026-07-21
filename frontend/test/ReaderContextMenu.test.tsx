import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ReaderContextMenu } from '../src/reader/ReaderContextMenu'

const rect = (x = 20, y = 30, width = 40, height = 10) => new DOMRect(x, y, width, height)

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('ReaderContextMenu', () => {
  it('portals an accessible menu and focuses its first enabled action', async () => {
    const { container } = render(
      <ReaderContextMenu
        open
        anchorRect={rect()}
        actions={[
          { id: 'disabled', label: '不可用', disabled: true, run: vi.fn() },
          { id: 'add', label: '加入待抓取（1）', run: vi.fn() },
        ]}
        onClose={vi.fn()}
      />,
    )

    const menu = screen.getByRole('menu')
    const add = screen.getByRole('menuitem', { name: '加入待抓取（1）' })
    expect(container.contains(menu)).toBe(false)
    await waitFor(() => expect(document.activeElement).toBe(add))
  })

  it('supports roving arrow focus plus Enter and Space execution', async () => {
    const first = vi.fn()
    const second = vi.fn()
    const onClose = vi.fn()
    render(
      <ReaderContextMenu
        open
        anchorRect={rect()}
        actions={[
          { id: 'first', label: '第一项', run: first },
          { id: 'disabled', label: '跳过', disabled: true, run: vi.fn() },
          { id: 'second', label: '第二项', run: second },
        ]}
        onClose={onClose}
      />,
    )
    const firstItem = screen.getByRole('menuitem', { name: '第一项' })
    const secondItem = screen.getByRole('menuitem', { name: '第二项' })
    await waitFor(() => expect(document.activeElement).toBe(firstItem))
    fireEvent.keyDown(firstItem, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(secondItem)
    fireEvent.keyDown(secondItem, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(firstItem)
    fireEvent.keyDown(firstItem, { key: 'Enter' })
    await waitFor(() => expect(first).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))

    onClose.mockClear()
    render(
      <ReaderContextMenu
        open
        anchorRect={rect()}
        actions={[{ id: 'second', label: '第二项新菜单', run: second }]}
        onClose={onClose}
      />,
    )
    const spaceItem = screen.getByRole('menuitem', { name: '第二项新菜单' })
    fireEvent.keyDown(spaceItem, { key: ' ' })
    await waitFor(() => expect(second).toHaveBeenCalledTimes(1))
  })

  it.each(['Escape', 'Tab'])('closes on %s', async (key) => {
    const onClose = vi.fn()
    render(
      <ReaderContextMenu
        open
        anchorRect={rect()}
        actions={[{ id: 'add', label: '加入', run: vi.fn() }]}
        onClose={onClose}
      />,
    )
    const item = screen.getByRole('menuitem')
    await waitFor(() => expect(document.activeElement).toBe(item))
    fireEvent.keyDown(item, { key })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes for an outside pointerdown but retains selection on an inside pointerdown', () => {
    const onClose = vi.fn()
    render(
      <ReaderContextMenu
        open
        anchorRect={rect()}
        actions={[{ id: 'add', label: '加入', run: vi.fn() }]}
        onClose={onClose}
      />,
    )
    const item = screen.getByRole('menuitem')
    expect(fireEvent.pointerDown(item)).toBe(false)
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.pointerDown(document.body)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('disables all actions while awaiting the active action', async () => {
    let resolve!: () => void
    const pending = new Promise<void>((done) => { resolve = done })
    render(
      <ReaderContextMenu
        open
        anchorRect={rect()}
        actions={[
          { id: 'add', label: '加入', run: () => pending },
          { id: 'copy', label: '复制', run: vi.fn() },
        ]}
        onClose={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('menuitem', { name: '加入' }))
    expect(screen.getByRole('menu').getAttribute('aria-busy')).toBe('true')
    expect((screen.getByRole('menuitem', { name: '加入' }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('menuitem', { name: '复制' }) as HTMLButtonElement).disabled).toBe(true)
    resolve()
    await waitFor(() => expect(screen.getByRole('menu').getAttribute('aria-busy')).toBe('false'))
  })

  it('stays open when an action rejects', async () => {
    const onClose = vi.fn()
    render(
      <ReaderContextMenu
        open
        anchorRect={rect()}
        actions={[{ id: 'add', label: '加入', run: () => Promise.reject(new Error('nope')) }]}
        onClose={onClose}
      />,
    )
    fireEvent.click(screen.getByRole('menuitem'))
    await waitFor(() => expect(screen.getByRole('menu').getAttribute('aria-busy')).toBe('false'))
    expect(onClose).not.toHaveBeenCalled()
    expect(document.body.contains(screen.getByRole('menu'))).toBe(true)
  })

  it('clamps its fixed position inside the viewport', async () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(rect(0, 0, 120, 80))
    vi.stubGlobal('innerWidth', 200)
    vi.stubGlobal('innerHeight', 160)
    render(
      <ReaderContextMenu
        open
        anchorRect={rect(195, 145, 10, 10)}
        actions={[{ id: 'add', label: '加入', run: vi.fn() }]}
        onClose={vi.fn()}
      />,
    )
    const menu = screen.getByRole('menu')
    await waitFor(() => {
      expect(menu.style.left).toBe('72px')
      expect(menu.style.top).toBe('57px')
    })
  })
})
