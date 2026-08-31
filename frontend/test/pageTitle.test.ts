import { describe, expect, it } from 'vitest'
import {
  buildDocumentTitle,
  getRoutePageTitle,
  type ArticleTitleLocationState,
} from '../src/utils/pageTitle'

describe('page title helpers', () => {
  it('formats static pages with the app name suffix', () => {
    expect(buildDocumentTitle('订阅源')).toBe('订阅源 - RSS Pal')
    expect(buildDocumentTitle('RSS Pal')).toBe('RSS Pal')
  })

  it('names article list variants so browser history can distinguish them', () => {
    expect(getRoutePageTitle('/articles', '')).toBe('文章列表')
    expect(getRoutePageTitle('/articles', '?saved=1')).toBe('已保存文章')
    expect(getRoutePageTitle('/articles', '?view=clip')).toBe('网摘')
    expect(getRoutePageTitle('/articles', '?feed_id=7')).toBe('订阅文章')
  })

  it('names the canonical interest path as 兴趣', () => {
    expect(getRoutePageTitle('/interests')).toBe('兴趣')
  })

  it('names explore list and detail paths independently from formal articles', () => {
    expect(getRoutePageTitle('/explore')).toBe('探索')
    expect(getRoutePageTitle('/explore/articles/19')).toBe('探索文章 19')
    expect(getRoutePageTitle('/explore/articles/19', '', {
      articlePreview: { id: 19, title: '候选文章' },
    })).toBe('候选文章')
  })

  it('uses article preview titles for article detail history entries', () => {
    const state: ArticleTitleLocationState = {
      articlePreview: {
        id: 42,
        title: '一篇能在回退列表里看懂的文章',
      },
    }

    expect(getRoutePageTitle('/articles/42', '', state)).toBe('一篇能在回退列表里看懂的文章')
  })

  it('falls back to the article id before the detail title is known', () => {
    expect(getRoutePageTitle('/articles/42', '')).toBe('文章 42')
  })
})
