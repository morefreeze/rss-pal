import { afterEach, beforeEach, expect, it } from 'vitest'
import { applyInitialTheme, getTheme } from './theme'

beforeEach(() => { localStorage.clear(); document.body.removeAttribute('data-theme') })
afterEach(() => { localStorage.clear(); document.body.removeAttribute('data-theme') })

it('starts new readers in the white quiet theme without writing a preference', () => {
  expect(getTheme()).toBe('quiet')
  applyInitialTheme()
  expect(document.body.getAttribute('data-theme')).toBe('quiet')
  expect(localStorage.getItem('rsspal:theme')).toBeNull()
})
it.each(['paper', 'quiet', 'pearl', 'night', 'neon'])('preserves the saved %s theme', theme => {
  localStorage.setItem('rsspal:theme', theme)
  applyInitialTheme()
  expect(document.body.getAttribute('data-theme')).toBe(theme)
  expect(localStorage.getItem('rsspal:theme')).toBe(theme)
})
it.each([['sepia', 'paper'], ['dark', 'night'], ['gray', 'pearl']])('keeps legacy %s readers in %s', (legacy, theme) => {
  localStorage.setItem('rsspal:reader-settings', JSON.stringify({ bgTheme: legacy }))
  expect(getTheme()).toBe(theme)
})
it('falls back to white for invalid or damaged preferences', () => {
  localStorage.setItem('rsspal:theme', 'unknown')
  localStorage.setItem('rsspal:reader-settings', '{broken')
  expect(getTheme()).toBe('quiet')
})
