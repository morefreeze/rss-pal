import { render } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'
import RoutePageTitle from '../src/components/RoutePageTitle'

describe('RoutePageTitle', () => {
  beforeEach(() => {
    document.title = 'RSS Pal'
  })

  it('writes distinguishable list titles to the browser history title', () => {
    render(
      <MemoryRouter initialEntries={['/articles?saved=1']}>
        <RoutePageTitle />
      </MemoryRouter>,
    )

    expect(document.title).toBe('已保存文章 - RSS Pal')
  })

  it('uses route state preview titles before an article detail request finishes', () => {
    render(
      <MemoryRouter initialEntries={[{
        pathname: '/articles/42',
        state: {
          articlePreview: {
            id: 42,
            title: '回退列表里能认出来的文章',
          },
        },
      }]}>
        <RoutePageTitle />
      </MemoryRouter>,
    )

    expect(document.title).toBe('回退列表里能认出来的文章 - RSS Pal')
  })

  it('names the explore list route', () => {
    render(
      <MemoryRouter initialEntries={['/explore']}>
        <RoutePageTitle />
      </MemoryRouter>,
    )

    expect(document.title).toBe('探索 - RSS Pal')
  })

  it('uses route state preview titles for explore article details', () => {
    render(
      <MemoryRouter initialEntries={[{
        pathname: '/explore/articles/19',
        state: {
          articlePreview: {
            id: 19,
            title: '候选源里的一篇文章',
          },
        },
      }]}>
        <RoutePageTitle />
      </MemoryRouter>,
    )

    expect(document.title).toBe('候选源里的一篇文章 - RSS Pal')
  })
})
