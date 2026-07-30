'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const protocol = require('./protocol');
const {
  RSS_ORIGIN,
  RSS_PAL_YOUTUBE_BRIDGE_PING,
  RSS_PAL_YOUTUBE_BRIDGE_READY,
  RSS_PAL_YOUTUBE_RESOLVE_REQUEST,
  RSS_PAL_YOUTUBE_RESOLVE_CANCEL,
  RSS_PAL_YOUTUBE_RESOLVE_RESPONSE,
  RUNTIME_RESOLVE,
  RUNTIME_CANCEL,
  isVideoId,
  isRequestId,
  validateRuntimeResolve,
  validateRuntimeCancel,
  isTrustedSender,
} = protocol;

test('exports the fixed YouTube bridge protocol constants', () => {
  assert.equal(RSS_ORIGIN, 'https://rss.morefreeze.top');
  assert.equal(RSS_PAL_YOUTUBE_BRIDGE_PING, 'RSS_PAL_YOUTUBE_BRIDGE_PING');
  assert.equal(RSS_PAL_YOUTUBE_BRIDGE_READY, 'RSS_PAL_YOUTUBE_BRIDGE_READY');
  assert.equal(RSS_PAL_YOUTUBE_RESOLVE_REQUEST, 'RSS_PAL_YOUTUBE_RESOLVE_REQUEST');
  assert.equal(RSS_PAL_YOUTUBE_RESOLVE_CANCEL, 'RSS_PAL_YOUTUBE_RESOLVE_CANCEL');
  assert.equal(RSS_PAL_YOUTUBE_RESOLVE_RESPONSE, 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE');
  assert.equal(RUNTIME_RESOLVE, 'rssPalYouTubeResolve');
  assert.equal(RUNTIME_CANCEL, 'rssPalYouTubeCancel');
});

test('freezes the exported protocol API', () => {
  assert.equal(Object.isFrozen(protocol), true);
});

test('keeps validation patterns private behind the frozen API', () => {
  assert.equal(Object.isFrozen(protocol), true);
  assert.equal(protocol.VIDEO_ID_RE, undefined);
  assert.equal(protocol.REQUEST_ID_RE, undefined);
  assert.equal(isVideoId('not-a-video-id'), false);
  assert.equal(isRequestId('unsafe request id'), false);
  assert.equal(
    validateRuntimeResolve({
      action: RUNTIME_RESOLVE,
      requestId: 'unsafe request id',
      videoId: 'not-a-video-id',
    }),
    null,
  );
});

test('installs the frozen protocol API on the browser global', () => {
  const source = fs.readFileSync(require.resolve('./protocol'), 'utf8');
  const context = vm.createContext({ URL });

  vm.runInContext(source, context);

  const browserProtocol = context.__rssPalYouTubeProtocol;
  assert.equal(Object.isFrozen(browserProtocol), true);
  assert.equal(browserProtocol.RSS_ORIGIN, RSS_ORIGIN);
  assert.equal(typeof browserProtocol.validateRuntimeResolve, 'function');
  assert.equal(browserProtocol.VIDEO_ID_RE, undefined);
  assert.equal(browserProtocol.REQUEST_ID_RE, undefined);
  assert.equal(
    Object.values(browserProtocol).every(
      (value) => typeof value === 'string' || typeof value === 'function',
    ),
    true,
  );
});

test('validates the exact runtime resolve message and returns only its payload', () => {
  assert.deepEqual(
    validateRuntimeResolve({
      action: RUNTIME_RESOLVE,
      requestId: 'req_01HX9X2M7T',
      videoId: 'dQw4w9WgXcQ',
    }),
    {
      requestId: 'req_01HX9X2M7T',
      videoId: 'dQw4w9WgXcQ',
    },
  );
});

test('rejects unsafe request IDs', () => {
  const unsafeRequestIds = [
    '',
    'has space',
    '../escape',
    'request.id',
    'a'.repeat(81),
    null,
  ];

  for (const requestId of unsafeRequestIds) {
    assert.equal(isRequestId(requestId), false);
    assert.equal(
      validateRuntimeResolve({
        action: RUNTIME_RESOLVE,
        requestId,
        videoId: 'dQw4w9WgXcQ',
      }),
      null,
    );
  }
});

test('rejects malformed video IDs and URLs passed as video IDs', () => {
  const malformedVideoIds = [
    '',
    'dQw4w9WgX',
    'dQw4w9WgXcQ1',
    'dQw4w9WgXc!',
    'https://youtu.be/dQw4w9WgXcQ',
    null,
  ];

  for (const videoId of malformedVideoIds) {
    assert.equal(isVideoId(videoId), false);
    assert.equal(
      validateRuntimeResolve({
        action: RUNTIME_RESOLVE,
        requestId: 'req_01HX9X2M7T',
        videoId,
      }),
      null,
    );
  }
});

test('rejects runtime resolve messages with extra keys', () => {
  for (const extra of [
    { url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ' },
    { script: 'alert(1)' },
    { selector: '#player' },
    { headers: { Authorization: 'secret' } },
    { cookie: 'SID=secret' },
  ]) {
    assert.equal(
      validateRuntimeResolve({
        action: RUNTIME_RESOLVE,
        requestId: 'req_01HX9X2M7T',
        videoId: 'dQw4w9WgXcQ',
        ...extra,
      }),
      null,
    );
  }
});

test('trusts only tab senders from the exact RSS Pal origin', () => {
  assert.equal(
    isTrustedSender({
      frameId: 0,
      url: 'https://rss.morefreeze.top/articles/42?view=reader',
      origin: RSS_ORIGIN,
      tab: {
        id: 17,
        url: 'https://rss.morefreeze.top/articles/42?view=reader',
      },
    }),
    true,
  );

  for (const sender of [
    {},
    {
      frameId: 0,
      url: 'https://rss.morefreeze.top/',
      tab: { url: 'https://rss.morefreeze.top/' },
    },
    {
      frameId: 0,
      url: 'https://rss.morefreeze.top/',
      tab: { id: '17', url: 'https://rss.morefreeze.top/' },
    },
    {
      frameId: 0,
      url: 'https://rss.morefreeze.top/',
      tab: { id: 17 },
    },
    {
      frameId: 0,
      url: 'https://rss.morefreeze.top/',
      tab: { id: 17, url: 'not a url' },
    },
    {
      frameId: 0,
      url: 'https://rss.morefreeze.top/',
      tab: { id: 17, url: 'http://rss.morefreeze.top/' },
    },
    {
      frameId: 0,
      url: 'https://rss.morefreeze.top/',
      tab: { id: 17, url: 'https://evil.example/' },
    },
    {
      frameId: 0,
      url: 'https://rss.morefreeze.top/',
      tab: { id: 17, url: 'https://rss.morefreeze.top.evil.example/' },
    },
  ]) {
    assert.equal(isTrustedSender(sender), false);
  }
});

test('rejects spoofed sender URLs even when the tab URL is trusted', () => {
  const trustedTab = {
    id: 17,
    url: 'https://rss.morefreeze.top/articles/42',
  };

  assert.equal(
    isTrustedSender({
      frameId: 0,
      url: 'https://evil.example/content-script',
      origin: RSS_ORIGIN,
      tab: trustedTab,
    }),
    false,
  );
  assert.equal(
    isTrustedSender({
      frameId: 0,
      url: 'https://rss.morefreeze.top/articles/42',
      origin: 'https://evil.example',
      tab: trustedTab,
    }),
    false,
  );
});

test('rejects trusted-origin senders from child frames', () => {
  assert.equal(
    isTrustedSender({
      frameId: 7,
      url: 'https://rss.morefreeze.top/articles/42',
      origin: RSS_ORIGIN,
      tab: {
        id: 17,
        url: 'https://rss.morefreeze.top/articles/42',
      },
    }),
    false,
  );
});

test('validates the exact runtime cancel message', () => {
  assert.deepEqual(
    validateRuntimeCancel({
      action: RUNTIME_CANCEL,
      requestId: 'req_01HX9X2M7T',
    }),
    {
      requestId: 'req_01HX9X2M7T',
    },
  );
});

test('rejects runtime cancel messages with extra keys', () => {
  assert.equal(
    validateRuntimeCancel({
      action: RUNTIME_CANCEL,
      requestId: 'req_01HX9X2M7T',
      videoId: 'dQw4w9WgXcQ',
    }),
    null,
  );
});
