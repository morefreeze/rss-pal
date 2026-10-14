import { it, expect } from 'vitest'
import { api, createOneoffLinkSet } from '../src/api/client'
it('allows the server capture budget instead of the default 10 second timeout',async()=>{
 const previous=api.defaults.adapter
 let timeout=0
 api.defaults.adapter=async config=>{timeout=config.timeout ?? 0;return {data:{feed_id:1,parent_article_id:42},status:201,statusText:'Created',headers:{},config}}
 try{expect(await createOneoffLinkSet('https://news.ycombinator.com/item?id=123',false)).toEqual({feed_id:1,parent_article_id:42});expect(timeout).toBe(35000)}finally{api.defaults.adapter=previous}
})
