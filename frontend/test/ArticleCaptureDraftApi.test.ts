import { expect, it, vi } from 'vitest'
import { api, getArticleCaptureDrafts, removeArticleCaptureDrafts } from '../src/api/client'

it('uses the normal authenticated API client for per-article draft GET and JSON DELETE', async () => {
  const data = {drafts:[{url:'https://example.com/shared',title:'Shared',addedAt:123}],fetched_link_urls:[]}
  const get = vi.spyOn(api, 'get').mockResolvedValue({data})
  const remove = vi.spyOn(api, 'delete').mockResolvedValue({status:204})
  expect(await getArticleCaptureDrafts(42)).toEqual(data)
  await removeArticleCaptureDrafts(42, ['https://example.com/shared'])
  expect(get).toHaveBeenCalledWith('/articles/42/capture-drafts')
  expect(remove).toHaveBeenCalledWith('/articles/42/capture-drafts', {data:{urls:['https://example.com/shared']}})
})
