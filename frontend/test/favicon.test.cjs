const { existsSync, readFileSync } = require('node:fs')
const { resolve } = require('node:path')

const indexHtml = readFileSync(resolve('index.html'), 'utf8')
const faviconPath = resolve('public/favicon.svg')
const pngFaviconPath = resolve('public/favicon-32.png')
const appleTouchIconPath = resolve('public/apple-touch-icon.png')

if (!indexHtml.includes('<link rel="icon" type="image/svg+xml" href="/favicon.svg" />')) {
  throw new Error('index.html should reference /favicon.svg as the site favicon')
}

if (indexHtml.includes('/vite.svg')) {
  throw new Error('index.html should not reference the default Vite favicon')
}

if (!indexHtml.includes('<link rel="alternate icon" type="image/png" sizes="32x32" href="/favicon-32.png" />')) {
  throw new Error('index.html should reference /favicon-32.png as a PNG fallback')
}

if (!indexHtml.includes('<link rel="apple-touch-icon" sizes="180x180" href="/apple-touch-icon.png" />')) {
  throw new Error('index.html should reference /apple-touch-icon.png for iOS home-screen icons')
}

if (!existsSync(faviconPath)) {
  throw new Error('public/favicon.svg should exist')
}

if (!existsSync(pngFaviconPath)) {
  throw new Error('public/favicon-32.png should exist for PNG fallback')
}

if (!existsSync(appleTouchIconPath)) {
  throw new Error('public/apple-touch-icon.png should exist for Apple touch icon support')
}

const favicon = readFileSync(faviconPath, 'utf8')

for (const expected of ['<title>RSS Pal</title>', 'rss-pal-favicon-bg', 'rss-pal-favicon-wave', 'rss-pal-favicon-progress']) {
  if (!favicon.includes(expected)) {
    throw new Error(`favicon.svg should include ${expected}`)
  }
}

console.log('favicon test passed')
