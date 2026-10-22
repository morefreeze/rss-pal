import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import ArticleSourceLink from '../src/components/ArticleSourceLink'
import ArticleProvenance from '../src/components/ArticleProvenance'

const child = { feed_id: 23, feed_title: '阮一峰的网络日志', url: 'https://deskmind.dev/zh/blog/launch', parent_article_id: 3248, parent_title: '科技爱好者周刊（第 415 期）', source: { key: 'host:deskmind.dev', title: 'deskmind.dev', url: 'https://deskmind.dev/zh/blog/launch' } }

describe('article source and discovery provenance', () => {
 it('links extracted sources to the original site without applying the parent feed filter', () => {
  const filter = vi.fn(), open = vi.fn()
  render(<div onClick={open}><ArticleSourceLink article={child} onNavigate={filter} /></div>)
  const source = screen.getByRole('link', { name: /deskmind.dev/ })
  expect(source.getAttribute('href')).toBe(child.url)
  expect(screen.queryByText('阮一峰的网络日志')).toBeNull()
  fireEvent.click(source)
  expect(filter).not.toHaveBeenCalled()
  expect(open).not.toHaveBeenCalled()
 })
 it('preserves normal subscription source filtering', () => {
  const filter = vi.fn()
  render(<ArticleSourceLink article={{ feed_id: 23, feed_title: '阮一峰的网络日志', url: 'https://ruanyifeng.com/415' }} onNavigate={filter} />)
  fireEvent.click(screen.getByRole('link'))
  expect(filter).toHaveBeenCalledWith(23, expect.stringContaining('feed_id=23'))
 })
 it('shows the parent title and links back without triggering the article card', () => {
  const open = vi.fn()
  render(<MemoryRouter><div onClick={open}><ArticleProvenance article={child} /></div></MemoryRouter>)
  expect(screen.getByText('来自这篇文章：')).toBeTruthy()
  const parent = screen.getByRole('link', {name:child.parent_title})
  expect(parent.getAttribute('href')).toBe('/articles/3248')
  fireEvent.click(parent)
  expect(open).not.toHaveBeenCalled()
 })
 it('has no discovery attribution for regular feed articles', () => {
  const {container}=render(<MemoryRouter><ArticleProvenance article={{}} /></MemoryRouter>)
  expect(container.textContent).toBe('')
 })
})
