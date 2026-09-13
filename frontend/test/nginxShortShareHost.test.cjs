const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')

const nginx = readFileSync(resolve('nginx.conf'), 'utf8')
const serverBlocks = nginx.split(/(?=^server\s*\{)/m).filter(block => /^server\s*\{/m.test(block))

function serverFor(name) {
  return serverBlocks.find(block => new RegExp(`server_name\\s+[^;]*\\b${name.replaceAll('.', '\\.')}\\b[^;]*;`).test(block))
}

const mainBlock = serverFor('rss.morefreeze.top')
if (!mainBlock) {
  throw new Error('nginx.conf should keep a main server for localhost and rss.morefreeze.top')
}
if (!mainBlock.includes('server_name localhost rss.morefreeze.top;')) {
  throw new Error('main server should name localhost and rss.morefreeze.top together')
}
if (!mainBlock.includes('access_log off;')) {
  throw new Error('main server should disable access logging')
}

const shortBlock = serverFor('r.morefreeze.top')
if (!shortBlock) {
  throw new Error('nginx.conf should define an isolated r.morefreeze.top server')
}

for (const expected of [
  'listen 80;',
  'listen 443 ssl;',
  'http2 on;',
  'server_name r.morefreeze.top;',
  'ssl_certificate /etc/nginx/certs/localhost+2.pem;',
  'ssl_certificate_key /etc/nginx/certs/localhost+2-key.pem;',
  'access_log off;',
  'location ^~ /api/s/',
  'location = /api/proxy/image',
  'location ^~ /assets/',
  'location = /favicon.svg',
  'location = /favicon-32.png',
  'location = /apple-touch-icon.png',
  'location ~ ^/[0-9A-Za-z]{12}$',
  'location ~* \\.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2)$',
  'try_files /index.html =404;',
  'add_header Cache-Control "no-store" always;',
  'add_header Referrer-Policy "no-referrer" always;',
  'add_header X-Content-Type-Options "nosniff" always;',
  'location /',
  'return 404;',
]) {
  if (!shortBlock.includes(expected)) {
    throw new Error(`short-share server should include: ${expected}`)
  }
}

if (/location\s+(?:\^~\s+)?\/api(?:\s|\{)/.test(shortBlock)) {
  throw new Error('short-share server must not expose a generic /api location')
}
for (const forbidden of ['/login', '/articles', '/status']) {
  if (shortBlock.includes(forbidden)) {
    throw new Error(`short-share server must not expose application route: ${forbidden}`)
  }
}

const proxyPasses = [...shortBlock.matchAll(/^\s*proxy_pass\s+([^;]+);/gm)].map(match => match[1])
if (proxyPasses.length !== 2 || proxyPasses.some(target => target !== '$upstream_api')) {
  throw new Error(`short-share server should have exactly two allowlisted API proxies, got: ${proxyPasses.join(', ')}`)
}
const upstreams = [...shortBlock.matchAll(/^\s*set\s+\$upstream_api\s+([^;]+);/gm)].map(match => match[1])
if (upstreams.length !== 2 || upstreams.some(target => target !== 'http://api:8080')) {
  throw new Error(`short-share API proxies should both resolve api:8080, got: ${upstreams.join(', ')}`)
}

const shortAPIIndex = shortBlock.indexOf('location ^~ /api/s/')
const staticExtensionIndex = shortBlock.indexOf('location ~* \\.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2)$')
if (shortAPIIndex < 0 || staticExtensionIndex < 0 || shortAPIIndex > staticExtensionIndex) {
  throw new Error('short-share API prefix should explicitly outrank the later static-extension regex')
}

console.log('nginx short-share host test passed')
