import { beforeEach, expect, it } from 'vitest'
import { saveListReturn, takeListReturn } from '../src/utils/articleListReturn'
beforeEach(() => { localStorage.clear(); sessionStorage.clear() })
const snapshot = { key: 'feed1', articles: [], offset: 40, hasMore: true, scrollY: 3200 }
it('restores pagination and scroll as one return snapshot exactly once', () => {
  saveListReturn(snapshot); sessionStorage.setItem('articleListScroll', '3200')
  expect(takeListReturn('feed1')).toMatchObject(snapshot)
  expect(takeListReturn('feed1')).toBeNull()
})
it('does not restore across accounts or filters', () => {
  saveListReturn(snapshot); sessionStorage.setItem('articleListScroll', '3200'); localStorage.setItem('user', 'other')
  expect(takeListReturn('feed1')).toBeNull()
  saveListReturn(snapshot); sessionStorage.setItem('articleListScroll', '3200')
  expect(takeListReturn('feed2')).toBeNull()
})
