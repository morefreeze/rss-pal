import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  logout: vi.fn(),
  resetArticleDetailCache: vi.fn(),
}))

vi.mock('../src/api/client', () => ({ logout: mocks.logout }))
vi.mock('../src/api/articleDetailCache', () => ({
  resetArticleDetailCache: mocks.resetArticleDetailCache,
}))

import { clearPrivateSessionState } from '../src/api/privateSession'

describe('private session cleanup', () => {
  beforeEach(() => vi.clearAllMocks())

  it('clears cached article bodies before auth logout', () => {
    const order: string[] = []
    mocks.resetArticleDetailCache.mockImplementation(() => order.push('cache'))
    mocks.logout.mockImplementation(() => order.push('auth'))

    clearPrivateSessionState()

    expect(order).toEqual(['cache', 'auth'])
  })
})
