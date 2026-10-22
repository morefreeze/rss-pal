const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')
const page = readFileSync(resolve('src/pages/FeedListPage.tsx'), 'utf8')
if (!page.includes('<SourceDirectory items={catalog}') || !page.includes('void doPreview(url)')) {
  throw new Error('public catalog must retain the existing preview action')
}
if (page.includes('getInitialPopularFeedsExpanded') || page.includes('popularFeedsExpanded')) {
  throw new Error('the directory must not use the legacy auto-collapse rule')
}
console.log('source directory integration check passed')
