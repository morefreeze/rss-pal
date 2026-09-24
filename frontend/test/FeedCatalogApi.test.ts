import { expect, it, vi } from 'vitest'
import { api } from '../src/api/client'
import { checkCatalogItem, publishCatalogItem, updateCatalogItem, listFeedCatalog } from '../src/api/feedCatalog'
it('sends explicit desired publication and revision, permits slow checks', async () => {
  const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { id: 3 } })
  await publishCatalogItem(3, false, 8)
  expect(post).toHaveBeenCalledWith('/admin/feed-catalog/3/publication', { published: false, revision: 8 }, { timeout: 90000 })
  await checkCatalogItem(3, 9)
  expect(post).toHaveBeenCalledWith('/admin/feed-catalog/3/check', { revision: 9 }, { timeout: 90000 })
})
it('reads the independent public collection and includes revision when editing', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({ data: [] })
  expect(await listFeedCatalog()).toEqual([])
  expect(get).toHaveBeenCalledWith('/feed-catalog')
  const put = vi.spyOn(api, 'put').mockResolvedValue({ data: { id: 3 } })
  const fields = { title: 'Title', url: 'https://example.com/rss', category: '', description: '', sort_order: 1, revision: 4 }
  await updateCatalogItem(3, fields)
  expect(put).toHaveBeenCalledWith('/admin/feed-catalog/3', fields)
})
