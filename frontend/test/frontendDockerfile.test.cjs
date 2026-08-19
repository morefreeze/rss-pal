const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')

const dockerfile = readFileSync(resolve('Dockerfile'), 'utf8')
const runtimeStage = dockerfile.slice(dockerfile.indexOf('# Runtime stage'))

if (runtimeStage.includes('apk add --no-cache zip')) {
  throw new Error('frontend runtime image should not install zip from Alpine package repositories')
}

for (const expected of [
  'COPY extension/ ./extension/',
  'node ./scripts/create-extension-zip.mjs',
  'COPY --from=builder /app/rss-pal-extension.zip /usr/share/nginx/html/rss-pal-extension.zip',
]) {
  if (!dockerfile.includes(expected)) {
    throw new Error(`frontend Dockerfile should include: ${expected}`)
  }
}

console.log('frontend Dockerfile extension zip test passed')
