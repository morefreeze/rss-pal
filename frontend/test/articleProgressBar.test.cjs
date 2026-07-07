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
  'ArticlePage should import the progress bar',
)
assertIncludes(
  articlePage,
  '<ArticleProgressBar',
  'ArticlePage should render the progress bar',
)
assertIncludes(
  articlePage,
  'historicalPercent={progressDisplay.historicalPercent}',
  'ArticlePage should drive the light-blue bar from saved high-water progress',
)
assertIncludes(
  articlePage,
  'currentPercent={progressDisplay.currentPercent}',
  'ArticlePage should drive the dark-blue bar from current viewport progress',
)
assertExcludes(
  articlePage,
  'aiMarker',
  'ArticlePage should not restore the old AI marker visual path',
)

assertIncludes(
  progressBar,
  'historicalPercent: number',
  'ArticleProgressBar should expose a historical progress prop',
)
assertIncludes(
  progressBar,
  'currentPercent: number',
  'ArticleProgressBar should expose a current progress prop',
)
assertIncludes(
  progressBar,
  'article-progress-fill-history',
  'ArticleProgressBar should render the light-blue historical fill',
)
assertIncludes(
  progressBar,
  'article-progress-fill-current',
  'ArticleProgressBar should render the dark-blue current-position fill',
)
assertExcludes(
  progressBar,
  'ConfettiBurst',
  'ArticleProgressBar should not include confetti',
)

assertIncludes(
  styles,
  '.article-progress-fill-history',
  'Progress styles should define the light-blue historical layer',
)
assertIncludes(
  styles,
  '.article-progress-fill-current',
  'Progress styles should define the dark-blue current layer',
)

console.log('articleProgressBar test passed')
