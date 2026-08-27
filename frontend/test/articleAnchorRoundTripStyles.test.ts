import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('article anchor round-trip styles', () => {
  it('has no unconditional transform that can overlap wrapped destination text', () => {
    const css = readFileSync(resolve('src/index.css'), 'utf8')
    const rule = css.match(/\.article-anchor-return-link\s*\{[^}]+\}/)?.[0] ?? ''

    expect(rule).not.toMatch(/transform\s*:/)
  })
})
