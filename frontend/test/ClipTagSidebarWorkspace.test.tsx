import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import ClipTagSidebar from '../src/components/ClipTagSidebar'
import type { ClipSelection } from '../src/components/ClipTagSidebar'

const workspace = vi.hoisted(() => ({ enabled: true }))
vi.mock('../src/workspace/ReadingWorkspaceContext', () => ({ useReadingWorkspace: () => workspace.enabled ? {} : null }))
const sources = Array.from({ length: 65 }, (_, index) => ({ key: `host:${index}`, title: `Source ${index}`, count: index + 1 }))
const tags = [{ id: 1, name: '技术', article_count: 8, created_at: '' }]
beforeEach(() => { workspace.enabled = true })

it('collapses long workspace source lists while leaving all and tag facets outside the disclosure', () => {
  const onSelect = vi.fn()
  const { container } = render(<ClipTagSidebar tags={tags} sources={sources} selection={{ kind: 'all' }} onSelect={onSelect} />)
  const disclosure = container.querySelector('details')!
  expect(disclosure).toBeTruthy()
  expect(disclosure.open).toBe(false)
  expect(screen.getByText('全部').closest('details')).toBeNull()
  expect(screen.getByText('技术').closest('details')).toBeNull()
  fireEvent.click(screen.getByText('来源'))
  expect(disclosure.open).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: /^Source 64\s*65$/ }))
  expect(onSelect).toHaveBeenCalledWith({ kind: 'source', key: 'host:64', title: 'Source 64' })
})

it('shows the selected source in the collapsed summary and preserves legacy standalone rendering', () => {
  const selection: ClipSelection = { kind: 'source', key: 'host:2', title: 'Source 2' }
  const { container, rerender } = render(<ClipTagSidebar tags={tags} sources={sources} selection={selection} onSelect={() => {}} />)
  expect(container.querySelector('summary')?.textContent).toContain('Source 2')
  expect(container.querySelector('details')?.open).toBe(false)
  workspace.enabled = false
  rerender(<ClipTagSidebar tags={tags} sources={sources} selection={selection} onSelect={() => {}} />)
  expect(container.querySelector('details')).toBeNull()
  expect(screen.getByText('Sources')).toBeTruthy()
  expect(screen.getByRole('button', { name: /^Source 64\s*65$/ })).toBeTruthy()
})
