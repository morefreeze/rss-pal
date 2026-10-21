import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import OverflowMenu from '../src/components/OverflowMenu'

it('anchors the phone overflow to the toolbar and bounds its nested feed picker', () => {
  const css = readFileSync('src/index.css', 'utf8')
  render(<div className="article-list-toolbar--compact"><OverflowMenu><input className="toolbar-control" /></OverflowMenu></div>)
  fireEvent.click(screen.getByRole('button', { name: '⋯' }))
  expect(screen.getByRole('menu').classList.contains('overflow-menu-panel')).toBe(true)
  expect(css).toMatch(/\.article-list-toolbar--compact\s+\.overflow-menu-root\s*\{[^}]*position:\s*static/)
  expect(css).toMatch(/\.article-list-toolbar--compact\s+\.feed-picker-popover\s*\{[^}]*width:\s*100%/)
  expect(css).toMatch(/\.article-list-toolbar--compact\s+input\.toolbar-control\s*\{[^}]*font-size:\s*16px/)
})

it('closes on Escape and outside clicks and keeps child interactions open', () => {
  render(<OverflowMenu><button>筛选</button></OverflowMenu>)
  const trigger = screen.getByRole('button', { name: '⋯' })
  fireEvent.click(trigger)
  fireEvent.click(screen.getByRole('button', { name: '筛选' }))
  expect(screen.queryByRole('menu')).not.toBeNull()
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('menu')).toBeNull()
  fireEvent.click(trigger)
  fireEvent.mouseDown(document.body)
  expect(screen.queryByRole('menu')).toBeNull()
})
