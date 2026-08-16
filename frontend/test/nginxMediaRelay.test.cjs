const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')

const nginx = readFileSync(resolve('nginx.conf'), 'utf8')
const genericAPILocation = 'location ^~ /api {'
const apiIndex = nginx.indexOf(genericAPILocation)

if (apiIndex < 0) {
  throw new Error('nginx.conf should define the generic /api location')
}

// Every media relay location that must stream end-to-end. Each gets the same
// assertions: exists, ordered before the generic /api block, and carries the
// full streaming header set.
const relayLocations = ['location ^~ /api/media/youtube/', 'location ^~ /api/media/audio/']

for (const mediaLocation of relayLocations) {
  const mediaIndex = nginx.indexOf(mediaLocation)
  if (mediaIndex < 0) {
    throw new Error(`nginx.conf should define a dedicated media relay location: ${mediaLocation}`)
  }
  if (mediaIndex > apiIndex) {
    throw new Error(`media relay location should appear before the generic /api location: ${mediaLocation}`)
  }

  const mediaBlock = nginx.slice(mediaIndex, apiIndex)
  for (const expected of [
    'proxy_set_header Range $http_range;',
    'proxy_set_header If-Range $http_if_range;',
    'proxy_buffering off;',
    'proxy_request_buffering off;',
    'proxy_cache off;',
    'proxy_max_temp_file_size 0;',
    'gzip off;',
    'proxy_read_timeout 21600s;',
    'proxy_send_timeout 21600s;',
  ]) {
    if (!mediaBlock.includes(expected)) {
      throw new Error(`media relay location ${mediaLocation} should include: ${expected}`)
    }
  }
}

console.log('nginx media relay test passed')
