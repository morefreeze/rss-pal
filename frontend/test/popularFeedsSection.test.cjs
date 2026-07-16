const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')

const feedListPage = readFileSync(resolve('src/pages/FeedListPage.tsx'), 'utf8')

for (const [expected, label] of [
  [
    "import { getInitialPopularFeedsExpanded } from '../utils/popularFeedsVisibility'",
    'page should import the visibility initializer',
  ],
  [
    'useState(getInitialPopularFeedsExpanded)',
    'page should lazily derive the initial whole-section state',
  ],
  [
    'aria-expanded={popularFeedsExpanded}',
    'whole-section button should expose its expanded state',
  ],
  [
    `<span aria-hidden="true" style={{ fontSize: 10 }}>{popularFeedsExpanded ? '▾' : '▸'}</span>`,
    'whole-section disclosure glyph should be hidden from the accessible name',
  ],
  [
    'setPopularFeedsExpanded(expanded => !expanded)',
    'whole-section button should toggle only session state',
  ],
  [
    '{popularFeedsExpanded && (',
    'category groups should render only while the whole section is expanded',
  ],
  [
    'setFoldedGroups(s => ({ ...s, [group.category]: !folded }))',
    'existing per-category folding should remain wired',
  ],
  [
    'onClick={() => { setNewUrl(f.url); doPreview(f.url) }}',
    'existing recommendation preview action should remain wired',
  ],
]) {
  if (!feedListPage.includes(expected)) {
    throw new Error(label)
  }
}

const popularFeedsContainerIndex = feedListPage.indexOf('<div id="popular-feeds-groups">')
const popularFeedsConditionalIndex = feedListPage.indexOf('{popularFeedsExpanded && (')
if (
  popularFeedsContainerIndex < 0 ||
  popularFeedsConditionalIndex < 0 ||
  popularFeedsContainerIndex >= popularFeedsConditionalIndex
) {
  throw new Error('popular feeds aria-controls target should remain mounted while collapsed')
}

console.log('popularFeedsSection wiring test passed')
