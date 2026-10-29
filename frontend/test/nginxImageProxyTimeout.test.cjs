const assert = require('node:assert/strict')
const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')

function locationBlock(server, directive) {
  const start = server.indexOf(`${directive} {`)
  assert.ok(start >= 0, `missing ${directive}`)
  const open = server.indexOf('{', start)
  let depth = 1
  let end = open + 1
  for (; end < server.length && depth; end++) {
    if (server[end] === '{') depth++
    if (server[end] === '}') depth--
  }
  assert.equal(depth, 0, 'unclosed image proxy location')
  return server.slice(start, end)
}

for (const path of ['nginx.conf', '../deploy/nginx/rss-pal-tencent.conf']) {
  const config = readFileSync(resolve(path), 'utf8')
  const servers = config.split(/(?=^server\s*\{)/m)
  for (const host of ['rss.morefreeze.top', 'r.morefreeze.top']) {
    const server = servers.find(block => new RegExp(`server_name\\s+[^;]*\\b${host.replaceAll('.', '\\.')}\\b[^;]*;`).test(block) && block.includes('listen 443'))
    assert.ok(server, `missing TLS server ${host} in ${path}`)
    const image = locationBlock(server, 'location = /api/proxy/image')
    assert.match(image, /proxy_pass\s+/)
    assert.match(image, /proxy_read_timeout\s+120s;/, `${path} ${host} must let the bounded 90s image fetch complete`)
  }
}
console.log('image proxy deadlines pass at both nginx hops and both hosts')
