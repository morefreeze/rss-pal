'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const protocol = require('./protocol');
const bridgeModule = require('./bridge-content');
const { createYouTubeContentBridge } = bridgeModule;

function createFakePage() {
  const listeners = [];
  const posts = [];

  return {
    listeners,
    posts,
    addEventListener(type, listener) {
      assert.equal(type, 'message');
      listeners.push(listener);
    },
    removeEventListener(type, listener) {
      assert.equal(type, 'message');
      const index = listeners.indexOf(listener);
      if (index !== -1) {
        listeners.splice(index, 1);
      }
    },
    postMessage(message, targetOrigin) {
      posts.push({ message, targetOrigin });
    },
    async dispatch(event) {
      await Promise.all([...listeners].map((listener) => listener(event)));
    },
  };
}

function createFakeRuntime(sendMessage) {
  return {
    getManifest() {
      return { version: '1.8.4' };
    },
    sendMessage,
  };
}

function trustedEvent(page, data) {
  return {
    source: page,
    origin: protocol.RSS_ORIGIN,
    data,
  };
}

test('exports only createYouTubeContentBridge through CommonJS', () => {
  assert.deepEqual(Object.keys(bridgeModule), ['createYouTubeContentBridge']);
  assert.equal(typeof createYouTubeContentBridge, 'function');
});

test('registers once and posts READY immediately and for PAGE_PING', async () => {
  const page = createFakePage();
  const runtime = createFakeRuntime(() => {
    throw new Error('sendMessage should not be called');
  });

  createYouTubeContentBridge(page, runtime, protocol);

  assert.equal(page.listeners.length, 1);
  assert.deepEqual(page.posts, [
    {
      message: {
        type: protocol.RSS_PAL_YOUTUBE_BRIDGE_READY,
        version: '1.8.4',
      },
      targetOrigin: protocol.RSS_ORIGIN,
    },
  ]);

  await page.dispatch(
    trustedEvent(page, {
      type: protocol.RSS_PAL_YOUTUBE_BRIDGE_PING,
    }),
  );

  assert.deepEqual(page.posts[1], {
    message: {
      type: protocol.RSS_PAL_YOUTUBE_BRIDGE_READY,
      version: '1.8.4',
    },
    targetOrigin: protocol.RSS_ORIGIN,
  });
});

test('forwards a resolve with an exact schema and preserves response correlation', async () => {
  const page = createFakePage();
  const runtimeMessages = [];
  const runtime = createFakeRuntime(async (message) => {
    runtimeMessages.push(message);
    return {
      ok: true,
      title: 'resolved',
      type: 'UNTRUSTED_OVERRIDE',
      requestId: 'wrong-request',
    };
  });

  createYouTubeContentBridge(page, runtime, protocol);
  await page.dispatch(
    trustedEvent(page, {
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_REQUEST,
      requestId: 'req_01HX9X2M7T',
      videoId: 'dQw4w9WgXcQ',
      url: 'https://evil.example/ignored',
    }),
  );

  assert.deepEqual(runtimeMessages, [
    {
      action: protocol.RUNTIME_RESOLVE,
      requestId: 'req_01HX9X2M7T',
      videoId: 'dQw4w9WgXcQ',
    },
  ]);
  assert.deepEqual(page.posts[1], {
    message: {
      ok: true,
      title: 'resolved',
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_RESPONSE,
      requestId: 'req_01HX9X2M7T',
    },
    targetOrigin: protocol.RSS_ORIGIN,
  });
});

test('maps runtime rejection to INTERNAL_ERROR without leaking exception text', async () => {
  const page = createFakePage();
  const runtime = createFakeRuntime(async () => {
    throw new Error('secret runtime details');
  });

  createYouTubeContentBridge(page, runtime, protocol);
  await page.dispatch(
    trustedEvent(page, {
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_REQUEST,
      requestId: 'request_2',
      videoId: 'dQw4w9WgXcQ',
    }),
  );

  assert.deepEqual(page.posts[1], {
    message: {
      ok: false,
      code: 'INTERNAL_ERROR',
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_RESPONSE,
      requestId: 'request_2',
    },
    targetOrigin: protocol.RSS_ORIGIN,
  });
  assert.equal(JSON.stringify(page.posts).includes('secret runtime details'), false);
});

test('ignores foreign origins, foreign sources, and non-object data', async () => {
  const page = createFakePage();
  const runtimeMessages = [];
  const runtime = createFakeRuntime(async (message) => {
    runtimeMessages.push(message);
    return { ok: true };
  });
  const request = {
    type: protocol.RSS_PAL_YOUTUBE_RESOLVE_REQUEST,
    requestId: 'request_3',
    videoId: 'dQw4w9WgXcQ',
  };

  createYouTubeContentBridge(page, runtime, protocol);
  await page.dispatch({
    source: page,
    origin: 'https://evil.example',
    data: request,
  });
  await page.dispatch({
    source: {},
    origin: protocol.RSS_ORIGIN,
    data: request,
  });
  await page.dispatch(trustedEvent(page, null));
  await page.dispatch(trustedEvent(page, 'not-an-object'));

  assert.deepEqual(runtimeMessages, []);
  assert.equal(page.posts.length, 1);
});

test('ignores malformed IDs and unknown or extra request types', async () => {
  const page = createFakePage();
  const runtimeMessages = [];
  const runtime = createFakeRuntime(async (message) => {
    runtimeMessages.push(message);
    return { ok: true };
  });

  createYouTubeContentBridge(page, runtime, protocol);
  for (const data of [
    {
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_REQUEST,
      requestId: 'unsafe request id',
      videoId: 'dQw4w9WgXcQ',
    },
    {
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_REQUEST,
      requestId: 'request_4',
      videoId: 'not-a-video-id',
    },
    {
      type: 'UNKNOWN_REQUEST',
      requestId: 'request_4',
      videoId: 'dQw4w9WgXcQ',
    },
    {
      type: `${protocol.RSS_PAL_YOUTUBE_RESOLVE_REQUEST}_EXTRA`,
      requestId: 'request_4',
      videoId: 'dQw4w9WgXcQ',
    },
  ]) {
    await page.dispatch(trustedEvent(page, data));
  }

  assert.deepEqual(runtimeMessages, []);
  assert.equal(page.posts.length, 1);
});

test('forwards valid cancellation exactly and swallows runtime rejection', async () => {
  const page = createFakePage();
  const runtimeMessages = [];
  const runtime = createFakeRuntime(async (message) => {
    runtimeMessages.push(message);
    if (message.requestId === 'cancel_reject') {
      throw new Error('ignored cancellation failure');
    }
    return { ok: true };
  });

  createYouTubeContentBridge(page, runtime, protocol);
  await page.dispatch(
    trustedEvent(page, {
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_CANCEL,
      requestId: 'cancel_ok',
      extra: 'not forwarded',
    }),
  );
  await page.dispatch(
    trustedEvent(page, {
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_CANCEL,
      requestId: 'cancel_reject',
    }),
  );
  await page.dispatch(
    trustedEvent(page, {
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_CANCEL,
      requestId: 'invalid cancel id',
    }),
  );

  assert.deepEqual(runtimeMessages, [
    {
      action: protocol.RUNTIME_CANCEL,
      requestId: 'cancel_ok',
    },
    {
      action: protocol.RUNTIME_CANCEL,
      requestId: 'cancel_reject',
    },
  ]);
  assert.equal(page.posts.length, 1);
});

test('destroy removes the exact registered listener', async () => {
  const page = createFakePage();
  const runtime = createFakeRuntime(() => {
    throw new Error('sendMessage should not be called');
  });

  const destroy = createYouTubeContentBridge(page, runtime, protocol);
  const registeredListener = page.listeners[0];

  assert.equal(typeof destroy, 'function');
  destroy();
  assert.equal(page.listeners.includes(registeredListener), false);
  assert.equal(page.listeners.length, 0);

  await page.dispatch(
    trustedEvent(page, {
      type: protocol.RSS_PAL_YOUTUBE_BRIDGE_PING,
    }),
  );
  assert.equal(page.posts.length, 1);
});

test('auto-installs in a browser global without CommonJS and posts READY', () => {
  const source = fs.readFileSync(require.resolve('./bridge-content'), 'utf8');
  const listeners = [];
  const posts = [];
  const page = {
    addEventListener(type, listener) {
      listeners.push({ type, listener });
    },
    removeEventListener() {},
    postMessage(message, targetOrigin) {
      posts.push({ message, targetOrigin });
    },
  };
  const context = vm.createContext({
    window: page,
    chrome: {
      runtime: {
        getManifest() {
          return { version: '1.8.4' };
        },
        sendMessage() {
          throw new Error('sendMessage should not be called');
        },
      },
    },
    __rssPalYouTubeProtocol: protocol,
  });

  vm.runInContext(source, context);

  assert.equal(listeners.length, 1);
  assert.equal(listeners[0].type, 'message');
  assert.equal(posts.length, 1);
  assert.equal(
    posts[0].message.type,
    protocol.RSS_PAL_YOUTUBE_BRIDGE_READY,
  );
  assert.equal(posts[0].message.version, '1.8.4');
  assert.equal(posts[0].targetOrigin, protocol.RSS_ORIGIN);
});
