const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')

const articlePage = readFileSync(resolve('src/pages/ArticlePage.tsx'), 'utf8')
const progressBar = readFileSync(resolve('src/components/ArticleProgressBar.tsx'), 'utf8')
const styles = readFileSync(resolve('src/index.css'), 'utf8')

function assertIncludes(source, pattern, message) {
  if (!source.includes(pattern)) {
    throw new Error(message)
  }
}

function assertExcludes(source, pattern, message) {
  if (source.includes(pattern)) {
    throw new Error(message)
  }
}

assertIncludes(
  articlePage,
  "import ArticleProgressBar from '../components/ArticleProgressBar'",
  'ArticlePage should import the historical progress bar',
)
assertIncludes(
  articlePage,
  '<ArticleProgressBar',
  'ArticlePage should render the historical progress bar',
)
assertIncludes(
  articlePage,
  'historicalPercent={progressDisplay.historicalPercent}',
  'ArticlePage should drive the top bar from saved high-water progress',
)
assertExcludes(
  articlePage,
  'currentPercent=',
  'ArticlePage should not pass current viewport progress into the top bar',
)
assertExcludes(
  articlePage,
  'aiMarker',
  'ArticlePage should not restore the old AI marker visual path',
)

assertIncludes(
  progressBar,
  'historicalPercent: number',
  'ArticleProgressBar should expose a single historical progress prop',
)
assertIncludes(
  progressBar,
  'article-progress-fill-history',
  'ArticleProgressBar should render the light-blue historical fill',
)
assertExcludes(
  progressBar,
  'currentPercent',
  'ArticleProgressBar should not render a current-position layer',
)
assertExcludes(
  progressBar,
  'article-progress-fill-current',
  'ArticleProgressBar should not render the dark-blue current-position fill',
)
assertExcludes(
  progressBar,
  'ConfettiBurst',
  'ArticleProgressBar should not include confetti while it is a historical-only bar',
)

assertExcludes(
  styles,
  '.article-progress-fill-current',
  'Progress styles should not define the removed dark-blue current layer',
)

console.log('articleHistoricalProgressBar test passed')
