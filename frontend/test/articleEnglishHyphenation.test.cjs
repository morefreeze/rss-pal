const assert = require('node:assert/strict')
const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')
const test = require('node:test')

const css = readFileSync(resolve('src/index.css'), 'utf8')

test('markdown body enables English hyphenation without arbitrary word splitting', () => {
  const rule = css.match(/\.markdown-body\s*\{[^}]+\}/)?.[0] ?? ''
  assert.match(rule, /hyphens:\s*auto\b/)
  assert.match(rule, /overflow-wrap:\s*break-word\b/)
  assert.match(rule, /word-break:\s*normal\b/)
})

test('English article prose uses justified lines with a natural final line', () => {
  const rule = css.match(/\.markdown-body\[lang="en"\]\s+:is\(p,\s*li\)\s*\{[^}]+\}/)?.[0] ?? ''
  assert.match(rule, /text-align:\s*justify\b/)
  assert.match(rule, /text-align-last:\s*start\b/)
})
