const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')

const articlePage = readFileSync(resolve('src/pages/ArticlePage.tsx'), 'utf8')

const blockedArticlePagePatterns = [
  ['ArticleProgressBar', 'ArticlePage should not render the top progress bars while they are disabled'],
  ['aiMarkerPos', 'ArticlePage should not keep AI marker state while top progress bars are disabled'],
  ['setAiMarkerPos', 'ArticlePage should not measure AI marker progress while top progress bars are disabled'],
  ['confettiFired', 'ArticlePage should not keep progress-bar confetti state while top progress bars are disabled'],
]

for (const [pattern, message] of blockedArticlePagePatterns) {
  if (articlePage.includes(pattern)) {
    throw new Error(message)
  }
}

console.log('articleProgressBarDisabled test passed')
