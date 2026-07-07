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
assertIncludes(
  articlePage,
  'const summaryRef = useRef<HTMLDivElement>(null)',
  'ArticlePage should keep a ref for measuring the AI summary card',
)
assertIncludes(
  articlePage,
  '<div ref={summaryRef} className="card">\n        <div className="flex-between mb-2">\n          <h3>AI 总结</h3>',
  'ArticlePage should attach summaryRef to the AI summary card',
)
assertIncludes(
  articlePage,
  'aiMarkerPercent={aiMarkerPos === null ? null : Math.min(100, Math.max(0, aiMarkerPos * 100))}',
  'ArticlePage should pass the AI summary marker position to the progress bar',
)
assertIncludes(
  articlePage,
  'aiMarker',
  'ArticlePage should restore the AI marker visual path',
)
assertExcludes(
  articlePage,
  'showCelebration',
  'ArticlePage should not restore the old confetti celebration state',
)
assertExcludes(
  articlePage,
  'confettiEnabled',
  'ArticlePage should not pass confetti settings to the progress bar',
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
  'aiMarkerPercent: number | null',
  'ArticleProgressBar should expose an AI summary marker prop',
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
assertIncludes(
  progressBar,
  '💡',
  'ArticleProgressBar should render the AI summary marker bulb',
)
assertExcludes(
  progressBar,
  'ConfettiBurst',
  'ArticleProgressBar should not include confetti',
)
assertExcludes(
  progressBar,
  'showCelebration',
  'ArticleProgressBar should not include old celebration state',
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
