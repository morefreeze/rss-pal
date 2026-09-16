import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import MarkdownArticle from './MarkdownArticle'
import { api } from '../api/client'
vi.mock('../api/client', () => ({ api: { get: vi.fn() } }))
afterEach(() => vi.restoreAllMocks())
it('loads private article figures through authenticated client and releases blob on unmount', async () => {
 const blob = new Blob(['png'], { type: 'image/png' })
 vi.mocked(api.get).mockResolvedValue({ data: blob })
 URL.createObjectURL = vi.fn(() => 'blob:private-figure')
 URL.revokeObjectURL = vi.fn()
 const view = render(<MarkdownArticle source="![figure](/api/articles/42/images/0.png)" />)
 await waitFor(() => expect(api.get).toHaveBeenCalledWith('/articles/42/images/0.png?__private=1', expect.objectContaining({ responseType: 'blob' })))
 await waitFor(() => expect(screen.getByAltText('figure').getAttribute('src')).toBe('blob:private-figure'))
 view.unmount()
 expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:private-figure')
})
it('keeps public share figure URLs directly usable without login', () => {
 const src='/api/s/AbCdEf123456/assets/0.png'
 render(<MarkdownArticle source={`![shared](${src})`} />)
 expect(screen.getByAltText('shared').getAttribute('src')).toBe(src)
})
it('never falls back to a raw private URL when access is denied', async () => {
 vi.mocked(api.get).mockRejectedValue(new Error('forbidden'))
 render(<MarkdownArticle source="![denied](/api/articles/99/images/0.png)" />)
 await waitFor(() => expect(api.get).toHaveBeenCalledWith('/articles/99/images/0.png?__private=1', expect.anything()))
 expect(screen.getByAltText('denied').hasAttribute('src')).toBe(false)
})
it('does not send authentication to lookalike external image paths', async () => {
 vi.mocked(api.get).mockClear()
 const src='https://evil.example/api/articles/42/images/0.png'
 render(<MarkdownArticle source={`![external](${src})`} />)
 expect(api.get).not.toHaveBeenCalled()
 expect(screen.getByAltText('external').getAttribute('src')).toBe(`/api/proxy/image?url=${encodeURIComponent(src)}`)
})

it('bypasses legacy immutable URLs and requests fresh authorization for each reader', async () => {
 vi.mocked(api.get).mockClear()
 const oldCached = new Blob(['old-owner-private-bytes'])
 const fresh = new Blob(['authorized-owner-bytes'])
 let owner = true
 vi.mocked(api.get).mockImplementation(async (url, options) => {
  if (url === '/articles/42/images/0.png') return { data: oldCached }
  expect(options?.headers).toMatchObject({ 'Cache-Control': 'no-cache, no-store', Pragma: 'no-cache' })
  if (!owner) throw new Error('403 userB')
  return { data: fresh }
 })
 URL.createObjectURL = vi.fn(() => 'blob:authorized-owner')
 URL.revokeObjectURL = vi.fn()
 const first = render(<MarkdownArticle source="![owner](/api/articles/42/images/0.png)" />)
 await waitFor(() => expect(screen.getByAltText('owner').getAttribute('src')).toBe('blob:authorized-owner'))
 first.unmount()
 owner = false
 render(<MarkdownArticle source="![other](/api/articles/42/images/0.png)" />)
 await waitFor(() => expect(api.get).toHaveBeenCalledTimes(2))
 expect(screen.getByAltText('other').hasAttribute('src')).toBe(false)
 expect(URL.createObjectURL).toHaveBeenCalledTimes(1)
 expect(URL.createObjectURL).toHaveBeenCalledWith(fresh)
})
