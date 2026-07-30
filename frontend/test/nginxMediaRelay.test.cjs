const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')

const nginx = readFileSync(resolve('nginx.conf'), 'utf8')
const mediaLocation = 'location ^~ /api/media/youtube/'
const genericAPILocation = 'location ^~ /api {'

const mediaIndex = nginx.indexOf(mediaLocation)
const apiIndex = nginx.indexOf(genericAPILocation)

if (mediaIndex < 0) {
  throw new Error('nginx.conf should define a dedicated YouTube media relay location')
}
if (apiIndex < 0 || mediaIndex > apiIndex) {
  throw new Error('YouTube media relay location should appear before the generic /api location')
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
    throw new Error(`YouTube media relay location should include: ${expected}`)
  }
}

console.log('nginx media relay test passed')
