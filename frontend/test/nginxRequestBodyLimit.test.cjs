const assert = require('node:assert/strict')
const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')
const test = require('node:test')

const nginx = readFileSync(resolve('nginx.conf'), 'utf8')

test('frontend proxy accepts capture bodies up to the API handoff limit', () => {
  assert.match(nginx, /^\s*client_max_body_size\s+5m;\s*$/m)
})
