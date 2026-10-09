import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  api,
  clearExploreNegativeFeedback,
  createExploreFeedback,
  deleteExploreFeedback,
  getExplore,
  getExploreArticle,
  getExploreSources,
  recordExploreArticleEvent,
  replaceExploreInterests,
  subscribeExploreSource,
  subscribeExploreSources,
} from '../src/api/client'

afterEach(() => {vi.restoreAllMocks();vi.useRealTimers()})

describe('Explore API client', () => {
  it('maps all ten Explore operations to their authenticated API endpoints', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: { kind: 'get' } })
    const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { kind: 'post' } })
    const put = vi.spyOn(api, 'put').mockResolvedValue({ data: { kind: 'put' } })
    const remove = vi.spyOn(api, 'delete').mockResolvedValue({ data: undefined })

    await getExplore({ limit: 20, offset: 0, sort: 'published', order: 'desc', topic: 'programming' })
    await getExploreSources()
    await getExploreArticle(23)
    await createExploreFeedback({ feedback_type: 'hide_source', source_id: 7 })
    await deleteExploreFeedback(17)
    await clearExploreNegativeFeedback()
    await replaceExploreInterests(['programming', 'security'])
    await recordExploreArticleEvent(23, 'completed_read')
    await subscribeExploreSource(7)
    await subscribeExploreSources([7, 8])

    expect(get).toHaveBeenNthCalledWith(1, '/explore', {
      params: { limit: 20, offset: 0, sort: 'published', order: 'desc', topic: 'programming' },
    })
    expect(get).toHaveBeenNthCalledWith(2, '/explore/sources')
    expect(get).toHaveBeenNthCalledWith(3, '/explore/articles/23')
    expect(post).toHaveBeenNthCalledWith(1, '/explore/feedback', {
      feedback_type: 'hide_source', source_id: 7,
    })
    expect(remove).toHaveBeenNthCalledWith(1, '/explore/feedback/17')
    expect(remove).toHaveBeenNthCalledWith(2, '/explore/feedback')
    expect(put).toHaveBeenCalledWith('/explore/interests', {
      topics: ['programming', 'security'],
    })
    expect(post).toHaveBeenNthCalledWith(2, '/explore/articles/23/events', {
      event_type: 'completed_read',
    })
    expect(post).toHaveBeenNthCalledWith(3, '/explore/sources/7/subscribe')
    expect(post).toHaveBeenNthCalledWith(4, '/explore/sources/subscribe-batch', {
      source_ids: [7, 8],
    })
  })
})

describe('Explore transient upstream recovery',()=>{
 it('recovers from deployment 502 without retrying writes',async()=>{
  vi.useFakeTimers();const get=vi.spyOn(api,'get').mockRejectedValueOnce({response:{status:502}}).mockResolvedValue({data:{articles:[]}});
  const result=getExplore();await vi.advanceTimersByTimeAsync(2000);await expect(result).resolves.toEqual({articles:[]});expect(get).toHaveBeenCalledTimes(2);
 });
 it('stops after bounded retries and does not retry auth failures',async()=>{
  vi.useFakeTimers();const error={response:{status:503}};const get=vi.spyOn(api,'get').mockRejectedValue(error);
  const result=getExploreSources().catch(e=>e);await vi.runAllTimersAsync();expect(await result).toBe(error);expect(get).toHaveBeenCalledTimes(4);
  get.mockClear().mockRejectedValue({response:{status:401}});await expect(getExplore()).rejects.toMatchObject({response:{status:401}});expect(get).toHaveBeenCalledTimes(1);
 });
});
