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

function createDeferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });

  return { promise, resolve, reject };
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

test('forwards a resolve and posts only the exact successful playback envelope', async () => {
  const page = createFakePage();
  const runtimeMessages = [];
  const playback = {
    url: 'https://media.example/videoplayback',
    mimeType: 'audio/mp4',
    expiresAt: 1780000000000,
  };
  const runtime = createFakeRuntime(async (message) => {
    runtimeMessages.push(message);
    return {
      ok: true,
      playback,
      title: 'must not cross the page boundary',
      error: 'must not cross the page boundary',
      stack: 'must not cross the page boundary',
      debug: { traceId: 'secret' },
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
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_RESPONSE,
      requestId: 'req_01HX9X2M7T',
      ok: true,
      playback,
    },
    targetOrigin: protocol.RSS_ORIGIN,
  });
});

test('allows only known resolved error codes and strips runtime debug fields', async () => {
  const page = createFakePage();
  const allowedCodes = [
    'LOGIN_REQUIRED',
    'VIDEO_UNAVAILABLE',
    'NO_SUPPORTED_FORMAT',
    'RESOLVE_TIMEOUT',
    'LOCAL_NETWORK_ERROR',
    'PLAYBACK_EXPIRED',
    'INTERNAL_ERROR',
  ];
  let responseIndex = 0;
  const runtime = createFakeRuntime(async () => ({
    ok: false,
    code: allowedCodes[responseIndex++],
    error: 'private runtime error',
    stack: 'private runtime stack',
    title: 'private runtime title',
    debug: { traceId: 'private' },
    type: 'UNTRUSTED_OVERRIDE',
    requestId: 'wrong-request',
  }));

  createYouTubeContentBridge(page, runtime, protocol);
  for (const [index, code] of allowedCodes.entries()) {
    const requestId = `resolved_error_${index}`;
    await page.dispatch(
      trustedEvent(page, {
        type: protocol.RSS_PAL_YOUTUBE_RESOLVE_REQUEST,
        requestId,
        videoId: 'dQw4w9WgXcQ',
      }),
    );

    assert.deepEqual(page.posts[index + 1], {
      message: {
        type: protocol.RSS_PAL_YOUTUBE_RESOLVE_RESPONSE,
        requestId,
        ok: false,
        code,
      },
      targetOrigin: protocol.RSS_ORIGIN,
    });
  }
});

test('maps malformed runtime responses and unknown codes to INTERNAL_ERROR', async () => {
  const page = createFakePage();
  const malformedResponses = [
    null,
    undefined,
    true,
    'failure',
    [],
    {},
    { ok: true },
    { ok: true, playback: null },
    { ok: true, playback: [] },
    { ok: true, playback: 'not-an-object' },
    { ok: false },
    { ok: false, code: 'UNKNOWN_RUNTIME_CODE', error: 'do not leak' },
    { ok: 'true', playback: {} },
  ];
  let responseIndex = 0;
  const runtime = createFakeRuntime(
    async () => malformedResponses[responseIndex++],
  );

  createYouTubeContentBridge(page, runtime, protocol);
  for (const [index] of malformedResponses.entries()) {
    const requestId = `malformed_${index}`;
    await page.dispatch(
      trustedEvent(page, {
        type: protocol.RSS_PAL_YOUTUBE_RESOLVE_REQUEST,
        requestId,
        videoId: 'dQw4w9WgXcQ',
      }),
    );

    assert.deepEqual(page.posts[index + 1], {
      message: {
        type: protocol.RSS_PAL_YOUTUBE_RESOLVE_RESPONSE,
        requestId,
        ok: false,
        code: 'INTERNAL_ERROR',
      },
      targetOrigin: protocol.RSS_ORIGIN,
    });
  }
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

test('destroy suppresses an in-flight resolve after runtime settlement', async () => {
  for (const outcome of ['fulfilled', 'rejected']) {
    const page = createFakePage();
    const deferred = createDeferred();
    const runtimeMessages = [];
    const runtime = createFakeRuntime((message) => {
      runtimeMessages.push(message);
      return deferred.promise;
    });

    const destroy = createYouTubeContentBridge(page, runtime, protocol);
    const dispatchPromise = page.dispatch(
      trustedEvent(page, {
        type: protocol.RSS_PAL_YOUTUBE_RESOLVE_REQUEST,
        requestId: `destroyed_${outcome}`,
        videoId: 'dQw4w9WgXcQ',
      }),
    );

    assert.deepEqual(runtimeMessages, [
      {
        action: protocol.RUNTIME_RESOLVE,
        requestId: `destroyed_${outcome}`,
        videoId: 'dQw4w9WgXcQ',
      },
    ]);
    destroy();

    if (outcome === 'fulfilled') {
      deferred.resolve({
        ok: true,
        playback: {
          url: 'https://media.example/videoplayback',
          mimeType: 'audio/mp4',
          expiresAt: 1780000000000,
        },
      });
    } else {
      deferred.reject(new Error('late runtime rejection'));
    }
    await dispatchPromise;

    assert.equal(page.posts.length, 1, outcome);
  }
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
