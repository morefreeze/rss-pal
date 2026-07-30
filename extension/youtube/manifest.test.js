const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'manifest.json'), 'utf8'));
const packageJson = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'));

function readCorsRules() {
  return JSON.parse(fs.readFileSync(
    path.join(root, 'rules', 'youtube-media-cors.json'),
    'utf8',
  ));
}

test('loads the YouTube bridge only on the production RSS Pal origin', () => {
  const bridges = manifest.content_scripts.filter((entry) =>
    entry.js.includes('youtube/bridge-content.js'));

  assert.equal(bridges.length, 1);
  assert.deepEqual(bridges[0].matches, ['https://rss.morefreeze.top/*']);
  assert.deepEqual(bridges[0].js, [
    'youtube/protocol.js',
    'youtube/bridge-content.js',
  ]);
});

test('requests DNR host access without cookie or debugger permissions', () => {
  assert.equal(manifest.version, '1.8.4');
  assert.equal(packageJson.version, '1.8.4');
  assert.equal(
    manifest.permissions.includes('declarativeNetRequestWithHostAccess'),
    true,
  );
  assert.equal(manifest.permissions.includes('cookies'), false);
  assert.equal(manifest.permissions.includes('debugger'), false);
  assert.deepEqual(manifest.host_permissions, ['<all_urls>']);
});

test('enables exactly one static YouTube media CORS ruleset', () => {
  assert.deepEqual(manifest.declarative_net_request, {
    rule_resources: [
      {
        id: 'youtube_media_cors',
        enabled: true,
        path: 'rules/youtube-media-cors.json',
      },
    ],
  });
});

test('limits the CORS rule to RSS Pal initiated GoogleVideo GET and HEAD XHR', () => {
  const rules = readCorsRules();

  assert.equal(rules.length, 1);
  const rule = rules[0];
  assert.equal(rule.id, 1);
  assert.equal(rule.priority, 1);
  assert.equal(rule.action.type, 'modifyHeaders');
  assert.deepEqual(rule.condition, {
    urlFilter: '||googlevideo.com/videoplayback',
    requestDomains: ['googlevideo.com'],
    initiatorDomains: ['rss.morefreeze.top'],
    resourceTypes: ['xmlhttprequest'],
    requestMethods: ['get', 'head'],
  });
});

test('sets only the approved RSS Pal CORS response headers', () => {
  const [rule] = readCorsRules();

  assert.deepEqual(rule.action.responseHeaders, [
    {
      header: 'Access-Control-Allow-Origin',
      operation: 'set',
      value: 'https://rss.morefreeze.top',
    },
    {
      header: 'Access-Control-Expose-Headers',
      operation: 'set',
      value: 'Accept-Ranges, Content-Length, Content-Range',
    },
    {
      header: 'Access-Control-Allow-Methods',
      operation: 'set',
      value: 'GET, HEAD',
    },
  ]);
});

test('keeps repeatable unit and smoke checks in package scripts', () => {
  assert.deepEqual(packageJson.scripts, {
    test: 'node --test youtube/*.test.js',
    smoke: 'node adapters/twitter/smoke-test.js',
    check: 'npm test && npm run smoke',
  });
});
