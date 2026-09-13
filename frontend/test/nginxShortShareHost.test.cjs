const { mkdirSync, mkdtempSync, readFileSync, rmSync } = require('node:fs')
const { tmpdir } = require('node:os')
const { join, resolve } = require('node:path')
const { spawnSync } = require('node:child_process')

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
]) {
  if (!shortBlock.includes(expected)) {
    throw new Error(`short-share server should include: ${expected}`)
  }
}

function locationFor(directive) {
  const marker = `    ${directive} {`
  const start = shortBlock.indexOf(marker)
  if (start < 0) {
    throw new Error(`short-share server should define: ${directive}`)
  }
  const next = shortBlock.indexOf('\n    location ', start + marker.length)
  return shortBlock.slice(start, next < 0 ? shortBlock.length : next)
}

const shortAPILocation = locationFor('location ^~ /api/s/')
const imageProxyLocation = locationFor('location = /api/proxy/image')
for (const [name, block] of [
  ['/api/s/', shortAPILocation],
  ['/api/proxy/image', imageProxyLocation],
]) {
  if (!block.includes('set $upstream_api http://api:8080;') || !block.includes('proxy_pass $upstream_api;')) {
    throw new Error(`${name} should proxy to api:8080 inside its own location`)
  }
  if ([...block.matchAll(/^\s*proxy_pass\s+/gm)].length !== 1) {
    throw new Error(`${name} should own exactly one proxy_pass`)
  }
}

const rootCodeLocation = locationFor('location ~ "^/[0-9A-Za-z]{12}$"')
for (const expected of [
  'try_files /index.html =404;',
  'add_header Cache-Control "no-store" always;',
  'add_header Referrer-Policy "no-referrer" always;',
  'add_header X-Content-Type-Options "nosniff" always;',
]) {
  if (!rootCodeLocation.includes(expected)) {
    throw new Error(`short-code root location should include: ${expected}`)
  }
}

const catchAllLocation = locationFor('location /')
if (!catchAllLocation.includes('return 404;')) {
  throw new Error('short-share catch-all location should return 404 inside its own block')
}

const assetLocation = locationFor('location ^~ /assets/')
for (const expected of ['try_files $uri =404;', 'add_header Cache-Control "public, immutable";']) {
  if (!assetLocation.includes(expected)) {
    throw new Error(`/assets/ location should include: ${expected}`)
  }
}
const faviconLocations = [
  locationFor('location = /favicon.svg'),
  locationFor('location = /favicon-32.png'),
  locationFor('location = /apple-touch-icon.png'),
]
if (faviconLocations.some(block => !block.includes('try_files $uri =404;'))) {
  throw new Error('each short-share favicon location should serve only its exact existing file')
}
const staticDenyLocation = locationFor('location ~* \\.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2)$')
if (!staticDenyLocation.includes('return 404;')) {
  throw new Error('short-share static-extension fallback should return 404 inside its own block')
}
const staticLocations = [assetLocation, ...faviconLocations, rootCodeLocation, staticDenyLocation]
if (staticLocations.some(block => block.includes('proxy_pass'))) {
  throw new Error('short-share static locations must never proxy requests')
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

const parserDir = mkdtempSync(join(tmpdir(), 'rss-pal-nginx-test-'))
const certDir = join(parserDir, 'certs')
mkdirSync(certDir)

try {
  const certificate = spawnSync('openssl', [
    'req', '-x509', '-nodes', '-newkey', 'rsa:2048',
    '-keyout', join(certDir, 'localhost+2-key.pem'),
    '-out', join(certDir, 'localhost+2.pem'),
    '-subj', '/CN=localhost',
    '-days', '1',
  ], { encoding: 'utf8' })
  if (certificate.status !== 0) {
    throw new Error(`could not generate temporary nginx test certificate:\n${certificate.stderr || certificate.stdout || certificate.error}`)
  }

  const parsed = spawnSync('docker', [
    'run', '--rm', '--entrypoint', 'nginx',
    '-v', `${resolve('nginx.conf')}:/etc/nginx/conf.d/default.conf:ro`,
    '-v', `${certDir}:/etc/nginx/certs:ro`,
    'nginx:alpine', '-t',
  ], { encoding: 'utf8' })
  if (parsed.status !== 0) {
    throw new Error(`real nginx parser rejected the deployment config:\n${parsed.stderr || parsed.stdout || parsed.error}`)
  }
  console.log((parsed.stderr || parsed.stdout).trim())
} finally {
  rmSync(parserDir, { recursive: true, force: true })
}

console.log('nginx short-share host test passed')
