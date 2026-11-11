import { beforeEach, expect, it } from 'vitest'
import { readWorkspaceDestinations, rememberWorkspaceDestination, moduleDestination } from './workspaceDestinations'
beforeEach(() => sessionStorage.clear())
it('restores module queries and separates accounts', () => {
  rememberWorkspaceDestination(1, 'articles', '/articles?feed=12&q=hello')
  rememberWorkspaceDestination(1, 'explore', '/explore?topic=tech')
  const paths = readWorkspaceDestinations(1)
  expect(moduleDestination('/articles', paths)).toBe('/articles?feed=12&q=hello')
  expect(moduleDestination('/explore', paths)).toBe('/explore?topic=tech')
  expect(readWorkspaceDestinations(2)).toEqual({})
})
it('discards external routes and paths assigned to the wrong module', () => {
  sessionStorage.setItem('reading-workspace-destinations:1', JSON.stringify({ articles: '/explore?q=hi', explore: 'https://evil.test', clips: '/articles?view=clip' }))
  expect(readWorkspaceDestinations(1)).toEqual({ clips: '/articles?view=clip' })
})
