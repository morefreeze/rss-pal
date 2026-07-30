'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const { selectPlayback } = require('./format-selection');
const protocol = require('./protocol');
const resolverApi = require('./resolver');

const VIDEO_ID = 'abc123DEF_-';
const OTHER_VIDEO_ID = 'Zyx987QWE_-';

function createEvent() {
  const listeners = new Set();
  return {
    addListener(listener) {
      listeners.add(listener);
    },
    removeListener(listener) {
      listeners.delete(listener);
    },
    emit(...args) {
      return [...listeners].map((listener) => listener(...args));
    },
    listenerCount() {
      return listeners.size;
    },
  };
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function sender(tabId = 7) {
  const url = 'https://rss.morefreeze.top/articles/42';
  return {
    tab: { id: tabId, url },
    frameId: 0,
    url,
    origin: 'https://rss.morefreeze.top',
  };
}

function resolveMessage(requestId, videoId = VIDEO_ID) {
  return {
    action: protocol.RUNTIME_RESOLVE,
    requestId,
    videoId,
  };
}

function cancelMessage(requestId) {
  return {
    action: protocol.RUNTIME_CANCEL,
    requestId,
  };
}

function createFakeChrome(options = {}) {
  const calls = {
    create: [],
    update: [],
    get: [],
    remove: [],
    executeScript: [],
    sessionGet: [],
    sessionSet: [],
    order: [],
  };
  const onUpdated = createEvent();
  const onRemoved = createEvent();
  const tabs = new Map();
  const sessionData = {
    ...(options.initialSessionData || {}),
  };
  let nextTabId = options.firstTabId || 101;

  const chromeApi = {
    tabs: {
      onUpdated,
      onRemoved,
      async create(details) {
        calls.create.push(details);
        calls.order.push('create');
        const created = options.create
          ? await options.create(details, calls.create.length)
          : { id: nextTabId++, status: 'loading' };
        if (created && Number.isInteger(created.id)) {
          tabs.set(created.id, { ...created });
        }
        return created;
      },
      async update(tabId, updateProperties) {
        calls.update.push({ tabId, updateProperties });
        calls.order.push('update');
        if (options.update) {
          return options.update(
            tabId,
            updateProperties,
            calls.update.length,
            tabs,
          );
        }
        const tab = tabs.get(tabId);
        if (!tab) {
          throw new Error('tab not found during update');
        }
        Object.assign(tab, updateProperties);
        return { ...tab };
      },
      async get(tabId) {
        calls.get.push(tabId);
        calls.order.push('get');
        if (options.get) {
          return options.get(tabId, tabs);
        }
        const tab = tabs.get(tabId);
        if (!tab) {
          throw new Error('tab not found with secret URL');
        }
        return { ...tab };
      },
      async remove(tabId) {
        calls.remove.push(tabId);
        calls.order.push('remove');
        if (options.remove) {
          await options.remove(
            tabId,
            calls.remove.length,
            tabs,
          );
        }
        tabs.delete(tabId);
        onRemoved.emit(tabId, { isWindowClosing: false });
      },
    },
    scripting: {
      async executeScript(details) {
        calls.executeScript.push(details);
        calls.order.push('executeScript');
        if (options.executeScript) {
          return options.executeScript(details, calls.executeScript.length);
        }
        return [{
          result: {
            status: 'OK',
            formats: [],
            resourceUrls: [],
          },
        }];
      },
    },
    storage: {
      session: {
        async get(keys) {
          calls.sessionGet.push(keys);
          if (options.sessionGet) {
            return options.sessionGet(keys, sessionData);
          }
          return { ...sessionData };
        },
        async set(values) {
          calls.sessionSet.push(structuredClone(values));
          if (options.sessionSet) {
            await options.sessionSet(
              values,
              calls.sessionSet.length,
              sessionData,
            );
          }
          Object.assign(sessionData, structuredClone(values));
        },
      },
    },
  };

  function completeTab(tabId) {
    const tab = tabs.get(tabId);
    if (tab) {
      tab.status = 'complete';
    }
    onUpdated.emit(tabId, { status: 'complete' }, tab);
  }

  return {
    chromeApi,
    calls,
    completeTab,
    events: { onUpdated, onRemoved },
    sessionData,
    tabs,
  };
}

async function waitFor(predicate, description = 'condition') {
  const deadline = Date.now() + 500;
  while (!predicate()) {
    if (Date.now() >= deadline) {
      assert.fail(`timed out waiting for ${description}`);
    }
    await new Promise((resolve) => setImmediate(resolve));
  }
}

function createResolver(fake, overrides = {}) {
  return resolverApi.createYouTubeResolver({
    chromeApi: fake.chromeApi,
    protocol,
    selectPlayback,
    capturePageState: async function capturePageState() {},
    now: () => 1_700_000_000_000,
    tabTimeoutMs: 100,
    captureTimeoutMs: 15_000,
    ...overrides,
  });
}

async function resolveAfterCompletingTab(fake, resolver, requestId) {
  const resolution = resolver.handleMessage(
    resolveMessage(requestId),
    sender(),
  );
  await waitFor(
    () => fake.events.onUpdated.listenerCount() === 1,
    'tab load listener',
  );
  fake.completeTab(101);
  return resolution;
}

test('exports the frozen resolver factory API', () => {
  assert.deepEqual(Object.keys(resolverApi), ['createYouTubeResolver']);
  assert.equal(typeof resolverApi.createYouTubeResolver, 'function');
  assert.equal(Object.isFrozen(resolverApi), true);
});

test('attaches the factory globally and returns frozen resolver methods', () => {
  const context = vm.createContext({
    URL,
    clearTimeout,
    setTimeout,
  });
  const source = fs.readFileSync(
    path.join(__dirname, 'resolver.js'),
    'utf8',
  );

  vm.runInContext(source, context);

  assert.equal(
    typeof context.__rssPalCreateYouTubeResolver,
    'function',
  );
  const resolver = resolverApi.createYouTubeResolver({});
  assert.deepEqual(Object.keys(resolver), [
    'handleMessage',
    'cancelRequestsForTab',
    'cleanupOrphans',
  ]);
  assert.equal(Object.isFrozen(resolver), true);
});

test('resolves through one canonical inactive tab and always cleans it up', async () => {
  const fake = createFakeChrome();
  const selected = {
    ok: true,
    playback: {
      mode: 'progressive',
      quality: 720,
      expiresAt: '2030-01-01T00:00:00.000Z',
      progressive: {
        url: 'https://rr1---sn.example.googlevideo.com/videoplayback',
        mimeType: 'video/mp4',
        height: 720,
      },
    },
  };
  const selectCalls = [];
  const capturePageState = async function capturePageState() {};
  const resolver = resolverApi.createYouTubeResolver({
    chromeApi: fake.chromeApi,
    protocol,
    selectPlayback(captured, nowMs) {
      selectCalls.push({ captured, nowMs });
      return selected;
    },
    capturePageState,
    now: () => 1_700_000_000_000,
    tabTimeoutMs: 100,
    captureTimeoutMs: 12_345,
  });

  const resolution = resolver.handleMessage(
    resolveMessage('request-one'),
    sender(),
  );
  await waitFor(
    () => fake.events.onUpdated.listenerCount() === 1,
    'tab load listener',
  );
  fake.completeTab(101);

  assert.deepEqual(await resolution, selected);
  assert.deepEqual(fake.calls.create, [{
    url: `https://www.youtube.com/watch?v=${VIDEO_ID}`,
    active: false,
  }]);
  assert.equal(fake.calls.executeScript.length, 1);
  assert.deepEqual(fake.calls.executeScript[0], {
    target: { tabId: 101 },
    world: 'MAIN',
    func: capturePageState,
    args: [{ timeoutMs: 12_345, videoId: VIDEO_ID }],
  });
  assert.deepEqual(selectCalls, [{
    captured: {
      status: 'OK',
      formats: [],
      resourceUrls: [],
    },
    nowMs: 1_700_000_000_000,
  }]);
  assert.deepEqual(fake.calls.remove, [101]);
  assert.deepEqual(
    fake.sessionData.rssPalYouTubeTabs,
    [],
  );
  assert.deepEqual(fake.calls.sessionSet, [
    { rssPalYouTubeTabs: [101] },
    { rssPalYouTubeTabs: [] },
  ]);
  assert.equal(fake.events.onUpdated.listenerCount(), 0);
  assert.equal(fake.events.onRemoved.listenerCount(), 0);
});

test('mutes the temporary tab immediately before load wait and injection', async () => {
  const fake = createFakeChrome();
  const resolver = createResolver(fake);
  const resolution = resolver.handleMessage(
    resolveMessage('tab-level-mute'),
    sender(),
  );
  await waitFor(
    () => fake.events.onUpdated.listenerCount() === 1,
    'muted tab load listener',
  );
  fake.completeTab(101);
  await resolution;

  assert.deepEqual(fake.calls.update, [{
    tabId: 101,
    updateProperties: { muted: true },
  }]);
  const createIndex = fake.calls.order.indexOf('create');
  const updateIndex = fake.calls.order.indexOf('update');
  const getIndex = fake.calls.order.indexOf('get');
  const injectionIndex = fake.calls.order.indexOf('executeScript');
  assert.equal(
    createIndex < updateIndex &&
      updateIndex < getIndex &&
      getIndex < injectionIndex,
    true,
  );
});

test('mute failure closes the tab without waiting or injecting', async () => {
  const fake = createFakeChrome({
    update: async () => {
      throw new Error(
        'private mute failure https://www.youtube.com/watch?v=secret',
      );
    },
  });
  const resolver = createResolver(fake);

  const result = await resolver.handleMessage(
    resolveMessage('mute-failure'),
    sender(),
  );

  assert.deepEqual(result, {
    ok: false,
    code: 'LOCAL_NETWORK_ERROR',
  });
  assert.deepEqual(fake.calls.update, [{
    tabId: 101,
    updateProperties: { muted: true },
  }]);
  assert.deepEqual(fake.calls.get, []);
  assert.deepEqual(fake.calls.executeScript, []);
  assert.deepEqual(fake.calls.remove, [101]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);
  assert.equal(JSON.stringify(result).includes('secret'), false);
});

test('cancellation during muting closes without load wait or injection', async () => {
  const muteStarted = deferred();
  const releaseMute = deferred();
  const fake = createFakeChrome({
    update: async () => {
      muteStarted.resolve();
      await releaseMute.promise;
      return { id: 101, muted: true };
    },
  });
  const resolver = createResolver(fake);
  const resolution = resolver.handleMessage(
    resolveMessage('cancel-during-mute'),
    sender(7),
  );
  await muteStarted.promise;

  assert.deepEqual(
    await resolver.handleMessage(
      cancelMessage('cancel-during-mute'),
      sender(7),
    ),
    { ok: true },
  );
  releaseMute.resolve();
  await resolution;

  assert.deepEqual(fake.calls.get, []);
  assert.deepEqual(fake.calls.executeScript, []);
  assert.equal(fake.calls.remove.includes(101), true);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);
});

test('returns LOGIN_REQUIRED and selection failures exactly', async (t) => {
  const cases = [
    {
      name: 'login required',
      captured: {
        status: 'LOGIN_REQUIRED',
        formats: [],
        resourceUrls: [],
      },
      expected: { ok: false, code: 'LOGIN_REQUIRED' },
    },
    {
      name: 'unplayable video',
      captured: {
        status: 'UNPLAYABLE',
        formats: [],
        resourceUrls: [],
      },
      expected: { ok: false, code: 'VIDEO_UNAVAILABLE' },
    },
  ];

  for (const [index, scenario] of cases.entries()) {
    await t.test(scenario.name, async () => {
      const fake = createFakeChrome({
        executeScript: async () => [{ result: scenario.captured }],
      });
      const resolver = createResolver(fake);
      const resultPromise = resolveAfterCompletingTab(
        fake,
        resolver,
        `selection-${index}`,
      );

      assert.deepEqual(await resultPromise, scenario.expected);
      assert.deepEqual(fake.calls.remove, [101]);
    });
  }
});

test('maps page capture timeout to RESOLVE_TIMEOUT', async () => {
  const fake = createFakeChrome({
    executeScript: async () => [{
      result: {
        status: 'CAPTURE_TIMEOUT',
        formats: [],
        resourceUrls: [],
      },
    }],
  });
  const resolver = createResolver(fake);

  const resolution = await resolveAfterCompletingTab(
    fake,
    resolver,
    'capture-timeout',
  );

  assert.deepEqual(await resolution, {
    ok: false,
    code: 'RESOLVE_TIMEOUT',
  });
  assert.deepEqual(fake.calls.remove, [101]);
});

test('maps a hard tab load timeout to RESOLVE_TIMEOUT and cleans listeners', async () => {
  const fake = createFakeChrome();
  const resolver = createResolver(fake, { tabTimeoutMs: 5 });

  const result = await resolver.handleMessage(
    resolveMessage('load-timeout'),
    sender(),
  );

  assert.deepEqual(result, {
    ok: false,
    code: 'RESOLVE_TIMEOUT',
  });
  assert.deepEqual(fake.calls.executeScript, []);
  assert.deepEqual(fake.calls.remove, [101]);
  assert.equal(fake.events.onUpdated.listenerCount(), 0);
  assert.equal(fake.events.onRemoved.listenerCount(), 0);
});

test('maps script rejection to sanitized LOCAL_NETWORK_ERROR', async () => {
  const fake = createFakeChrome({
    executeScript: async () => {
      throw new Error(
        'private chrome failure https://www.youtube.com/watch?v=secret',
      );
    },
  });
  const resolver = createResolver(fake);

  const resolution = await resolveAfterCompletingTab(
    fake,
    resolver,
    'script-rejection',
  );
  const result = await resolution;

  assert.deepEqual(result, {
    ok: false,
    code: 'LOCAL_NETWORK_ERROR',
  });
  assert.equal(JSON.stringify(result).includes('secret'), false);
  assert.equal(JSON.stringify(result).includes('youtube.com'), false);
  assert.deepEqual(fake.calls.remove, [101]);
});

test('maps missing or malformed injection results to INTERNAL_ERROR', async (t) => {
  const cases = [
    { name: 'empty injection array', injected: [] },
    { name: 'missing result', injected: [{}] },
    { name: 'primitive result', injected: [{ result: 'secret URL' }] },
    {
      name: 'missing capture collections',
      injected: [{ result: { status: 'OK' } }],
    },
    {
      name: 'empty capture status',
      injected: [{
        result: { status: '', formats: [], resourceUrls: [] },
      }],
    },
    {
      name: 'non-array formats',
      injected: [{
        result: {
          status: 'OK',
          formats: {},
          resourceUrls: [],
        },
      }],
    },
  ];

  for (const [index, scenario] of cases.entries()) {
    await t.test(scenario.name, async () => {
      const fake = createFakeChrome({
        executeScript: async () => scenario.injected,
      });
      const resolver = createResolver(fake);
      const resolution = await resolveAfterCompletingTab(
        fake,
        resolver,
        `malformed-${index}`,
      );

      assert.deepEqual(await resolution, {
        ok: false,
        code: 'INTERNAL_ERROR',
      });
      assert.deepEqual(fake.calls.remove, [101]);
    });
  }
});

test('rejects untrusted senders and invalid runtime schemas without a tab', async (t) => {
  const invalidCases = [
    {
      name: 'null message',
      message: null,
      sender: sender(),
    },
    {
      name: 'missing action',
      message: {},
      sender: sender(),
    },
    {
      name: 'array message',
      message: [],
      sender: sender(),
    },
    {
      name: 'untrusted resolve sender',
      message: resolveMessage('untrusted'),
      sender: {
        ...sender(),
        frameId: 1,
      },
    },
    {
      name: 'invalid resolve schema',
      message: {
        ...resolveMessage('invalid-resolve'),
        extra: true,
      },
      sender: sender(),
    },
    {
      name: 'invalid cancel schema',
      message: {
        ...cancelMessage('invalid-cancel'),
        videoId: VIDEO_ID,
      },
      sender: sender(),
    },
    {
      name: 'untrusted unknown action is rejected first',
      message: { action: 'not-a-youtube-action' },
      sender: {
        ...sender(),
        url: 'https://evil.example/steal',
      },
    },
  ];

  for (const scenario of invalidCases) {
    await t.test(scenario.name, async () => {
      const fake = createFakeChrome();
      const resolver = createResolver(fake);

      assert.deepEqual(
        await resolver.handleMessage(scenario.message, scenario.sender),
        { ok: false, code: 'INTERNAL_ERROR' },
      );
      assert.deepEqual(fake.calls.create, []);
    });
  }
});

test('returns null for an unknown action from a trusted sender', () => {
  const fake = createFakeChrome();
  const resolver = createResolver(fake);

  assert.equal(
    resolver.handleMessage(
      { action: 'not-a-youtube-action', arbitraryUrl: 'secret' },
      sender(),
    ),
    null,
  );
  assert.deepEqual(fake.calls.create, []);
});

test('deduplicates concurrent requests for the same video into one promise', async () => {
  const fake = createFakeChrome();
  const resolver = createResolver(fake);

  const first = resolver.handleMessage(
    resolveMessage('same-video-one'),
    sender(7),
  );
  const second = resolver.handleMessage(
    resolveMessage('same-video-two'),
    sender(8),
  );
  await new Promise((resolve) => setImmediate(resolve));
  const samePromise = first === second;
  const createCount = fake.calls.create.length;
  for (const tabId of [...fake.tabs.keys()]) {
    fake.completeTab(tabId);
  }
  const results = await Promise.all([first, second]);

  assert.equal(samePromise, true);
  assert.equal(createCount, 1);
  assert.equal(fake.calls.executeScript.length, 1);
  assert.deepEqual(results, [
    { ok: false, code: 'NO_SUPPORTED_FORMAT' },
    { ok: false, code: 'NO_SUPPORTED_FORMAT' },
  ]);
  assert.deepEqual(fake.calls.remove, [101]);
});

test('allows two different active videos and rejects the third without a tab', async () => {
  const fake = createFakeChrome();
  const resolver = createResolver(fake, { maxConcurrent: 2 });
  const thirdVideoId = 'ThirdVid0_-';

  const first = resolver.handleMessage(
    resolveMessage('different-one', VIDEO_ID),
    sender(7),
  );
  const second = resolver.handleMessage(
    resolveMessage('different-two', OTHER_VIDEO_ID),
    sender(8),
  );
  await waitFor(
    () => fake.events.onUpdated.listenerCount() >= 2,
    'two active load listeners',
  );
  const third = resolver.handleMessage(
    resolveMessage('different-three', thirdVideoId),
    sender(9),
  );
  await new Promise((resolve) => setImmediate(resolve));
  const tabsBeforeCompletion = fake.calls.create.length;
  for (const tabId of [...fake.tabs.keys()]) {
    fake.completeTab(tabId);
  }

  assert.deepEqual(await third, {
    ok: false,
    code: 'INTERNAL_ERROR',
  });
  assert.equal(tabsBeforeCompletion, 2);
  assert.deepEqual(await Promise.all([first, second]), [
    { ok: false, code: 'NO_SUPPORTED_FORMAT' },
    { ok: false, code: 'NO_SUPPORTED_FORMAT' },
  ]);
  assert.equal(fake.calls.executeScript.length, 2);
  assert.deepEqual(
    [...fake.calls.remove].sort((left, right) => left - right),
    [101, 102],
  );
});

test('cancelled unresolved entries retain both slots until their runs settle', async () => {
  const firstCreation = deferred();
  const secondCreation = deferred();
  const fake = createFakeChrome({
    create: async (_details, callNumber) => {
      if (callNumber === 1) {
        return firstCreation.promise;
      }
      if (callNumber === 2) {
        return secondCreation.promise;
      }
      return {
        id: 100 + callNumber,
        status: 'complete',
      };
    },
  });
  const resolver = createResolver(fake, { maxConcurrent: 2 });
  const thirdVideoId = 'ThirdVid0_-';
  const first = resolver.handleMessage(
    resolveMessage('cancelled-slot-one', VIDEO_ID),
    sender(7),
  );
  const second = resolver.handleMessage(
    resolveMessage('cancelled-slot-two', OTHER_VIDEO_ID),
    sender(8),
  );
  await waitFor(
    () => fake.calls.create.length === 2,
    'two unresolved tab creations',
  );

  assert.deepEqual(
    await resolver.handleMessage(
      cancelMessage('cancelled-slot-one'),
      sender(7),
    ),
    { ok: true },
  );
  assert.deepEqual(
    await resolver.handleMessage(
      cancelMessage('cancelled-slot-two'),
      sender(8),
    ),
    { ok: true },
  );
  const thirdWhileCancelled = await resolver.handleMessage(
    resolveMessage('blocked-third-slot', thirdVideoId),
    sender(9),
  );
  const sameVideoWhileCancelled = await resolver.handleMessage(
    resolveMessage('blocked-cancelled-video', VIDEO_ID),
    sender(10),
  );
  const createCountWhileCancelled = fake.calls.create.length;

  firstCreation.resolve({ id: 101, status: 'loading' });
  secondCreation.resolve({ id: 102, status: 'loading' });
  await Promise.all([first, second]);

  const retry = await resolver.handleMessage(
    resolveMessage('retry-after-cancelled-runs', VIDEO_ID),
    sender(11),
  );

  assert.deepEqual(thirdWhileCancelled, {
    ok: false,
    code: 'INTERNAL_ERROR',
  });
  assert.deepEqual(sameVideoWhileCancelled, {
    ok: false,
    code: 'INTERNAL_ERROR',
  });
  assert.equal(createCountWhileCancelled, 2);
  assert.equal(fake.calls.create.length, 3);
  assert.deepEqual(retry, {
    ok: false,
    code: 'NO_SUPPORTED_FORMAT',
  });
});

test('only the requester tab can cancel its request ID', async () => {
  const fake = createFakeChrome();
  const resolver = createResolver(fake);
  const resolution = resolver.handleMessage(
    resolveMessage('requester-owned-cancel'),
    sender(7),
  );
  await waitFor(
    () => fake.events.onUpdated.listenerCount() === 1,
    'requester-owned resolution',
  );

  assert.deepEqual(
    await resolver.handleMessage(
      cancelMessage('requester-owned-cancel'),
      sender(8),
    ),
    { ok: true },
  );
  assert.deepEqual(
    await resolver.handleMessage(
      cancelMessage('unknown-request'),
      sender(8),
    ),
    { ok: true },
  );
  assert.deepEqual(fake.calls.remove, []);

  fake.completeTab(101);
  assert.deepEqual(await resolution, {
    ok: false,
    code: 'NO_SUPPORTED_FORMAT',
  });
  assert.equal(fake.calls.executeScript.length, 1);
  assert.deepEqual(fake.calls.remove, [101]);

  assert.deepEqual(
    await resolver.handleMessage(
      cancelMessage('requester-owned-cancel'),
      sender(7),
    ),
    { ok: true },
  );
  assert.deepEqual(fake.calls.remove, [101]);
});

test('canceling one shared waiter keeps the temporary tab for the survivor', async () => {
  const fake = createFakeChrome();
  const resolver = createResolver(fake);
  const first = resolver.handleMessage(
    resolveMessage('shared-cancel-one'),
    sender(7),
  );
  const second = resolver.handleMessage(
    resolveMessage('shared-cancel-two'),
    sender(8),
  );
  await waitFor(
    () => fake.events.onUpdated.listenerCount() === 1,
    'shared tab load listener',
  );

  assert.deepEqual(
    await resolver.handleMessage(
      cancelMessage('shared-cancel-one'),
      sender(7),
    ),
    { ok: true },
  );
  assert.deepEqual(fake.calls.remove, []);

  fake.completeTab(101);
  assert.deepEqual(await Promise.all([first, second]), [
    { ok: false, code: 'NO_SUPPORTED_FORMAT' },
    { ok: false, code: 'NO_SUPPORTED_FORMAT' },
  ]);
  assert.equal(fake.calls.executeScript.length, 1);
  assert.deepEqual(fake.calls.remove, [101]);
});

test('canceling the last waiter closes the tab and removes load listeners', async () => {
  const fake = createFakeChrome();
  const resolver = createResolver(fake, { tabTimeoutMs: 1_000 });
  const resolution = resolver.handleMessage(
    resolveMessage('last-waiter'),
    sender(7),
  );
  await waitFor(
    () => fake.events.onUpdated.listenerCount() === 1,
    'last waiter tab load listener',
  );

  assert.deepEqual(
    await resolver.handleMessage(
      cancelMessage('last-waiter'),
      sender(7),
    ),
    { ok: true },
  );
  await waitFor(
    () => fake.calls.remove.includes(101),
    'last waiter tab close',
  );

  await resolution;
  assert.deepEqual(fake.calls.executeScript, []);
  assert.equal(fake.events.onUpdated.listenerCount(), 0);
  assert.equal(fake.events.onRemoved.listenerCount(), 0);
});

test('cancel before tabs.create resolves closes the later tab without injection', async () => {
  const creation = deferred();
  const fake = createFakeChrome({
    create: async () => creation.promise,
  });
  const resolver = createResolver(fake, { tabTimeoutMs: 1_000 });
  const resolution = resolver.handleMessage(
    resolveMessage('cancel-before-create'),
    sender(7),
  );

  assert.deepEqual(
    await resolver.handleMessage(
      cancelMessage('cancel-before-create'),
      sender(7),
    ),
    { ok: true },
  );
  creation.resolve({ id: 101, status: 'loading' });
  await waitFor(
    () => fake.calls.remove.includes(101),
    'deferred canceled tab close',
  );
  await resolution;

  assert.deepEqual(fake.calls.executeScript, []);
  assert.deepEqual(fake.calls.update, []);
  assert.equal(fake.calls.remove.includes(101), true);
  assert.equal(fake.events.onUpdated.listenerCount(), 0);
  assert.equal(fake.events.onRemoved.listenerCount(), 0);
});

test('requester tab removal cancels all its requests but preserves other waiters', async () => {
  const fake = createFakeChrome();
  const resolver = createResolver(fake, { maxConcurrent: 2 });
  const sharedFromClosedTab = resolver.handleMessage(
    resolveMessage('closed-tab-shared', VIDEO_ID),
    sender(7),
  );
  const sharedSurvivor = resolver.handleMessage(
    resolveMessage('surviving-tab-shared', VIDEO_ID),
    sender(8),
  );
  const onlyClosedTab = resolver.handleMessage(
    resolveMessage('closed-tab-only', OTHER_VIDEO_ID),
    sender(7),
  );
  await waitFor(
    () => fake.events.onUpdated.listenerCount() === 2,
    'requester tab resolutions',
  );

  await resolver.cancelRequestsForTab(7);
  await waitFor(
    () => fake.calls.remove.includes(102),
    'requester-only temporary tab close',
  );
  assert.equal(fake.calls.remove.includes(101), false);

  fake.completeTab(101);
  assert.deepEqual(
    await Promise.all([sharedFromClosedTab, sharedSurvivor]),
    [
      { ok: false, code: 'NO_SUPPORTED_FORMAT' },
      { ok: false, code: 'NO_SUPPORTED_FORMAT' },
    ],
  );
  await onlyClosedTab;
  assert.equal(fake.calls.executeScript.length, 1);
  assert.equal(fake.calls.remove.includes(101), true);

  assert.equal(
    resolver.cancelRequestsForTab('7'),
    undefined,
  );
});

test('normal close retains a transient failure for later orphan cleanup', async () => {
  let failNextRemove = true;
  const fake = createFakeChrome({
    remove: async (tabId) => {
      if (tabId === 101 && failNextRemove) {
        failNextRemove = false;
        throw new Error('temporary tab remove failure');
      }
    },
  });
  const resolver = createResolver(fake);

  await resolveAfterCompletingTab(
    fake,
    resolver,
    'transient-normal-close',
  );

  assert.deepEqual(fake.calls.remove, [101]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, [101]);

  await resolver.cleanupOrphans();

  assert.deepEqual(fake.calls.remove, [101, 101]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);
});

test('orphan cleanup retries transient failures and filters invalid session IDs', async () => {
  let failNextRemove = true;
  const fake = createFakeChrome({
    initialSessionData: {
      rssPalYouTubeTabs: [4, 4, 7, 0, -1, 1.5, '8', null],
    },
    remove: async (tabId) => {
      if (tabId === 7 && failNextRemove) {
        failNextRemove = false;
        throw new Error('private remove failure');
      }
    },
  });
  const resolver = createResolver(fake);

  await resolver.cleanupOrphans();

  assert.deepEqual(fake.calls.sessionGet, [
    ['rssPalYouTubeTabs'],
  ]);
  assert.deepEqual(fake.calls.remove, [4, 7]);
  assert.deepEqual(fake.calls.sessionSet, [
    { rssPalYouTubeTabs: [7] },
  ]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, [7]);

  await resolver.cleanupOrphans();

  assert.deepEqual(fake.calls.remove, [4, 7, 7]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);
});

test('normal close forgets a tab confirmed missing by Chrome', async () => {
  const fake = createFakeChrome({
    remove: async (tabId, _callNumber, tabs) => {
      tabs.delete(tabId);
      throw new Error(`No tab with id: ${tabId}.`);
    },
  });
  const resolver = createResolver(fake);

  await resolveAfterCompletingTab(fake, resolver, 'missing-normal-close');

  assert.deepEqual(fake.calls.remove, [101]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);

  await resolver.cleanupOrphans();

  assert.deepEqual(fake.calls.remove, [101]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);
});

test('orphan cleanup forgets a tab confirmed missing by Chrome', async () => {
  const fake = createFakeChrome({
    initialSessionData: {
      rssPalYouTubeTabs: [9],
    },
    remove: async (tabId) => {
      throw new Error(`Invalid tab ID: ${tabId}.`);
    },
  });
  const resolver = createResolver(fake);

  await resolver.cleanupOrphans();

  assert.deepEqual(fake.calls.remove, [9]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);

  await resolver.cleanupOrphans();

  assert.deepEqual(fake.calls.remove, [9]);
});

test('orphan cleanup preserves tabs that become active concurrently', async () => {
  const getStarted = deferred();
  const releaseGet = deferred();
  const fake = createFakeChrome({
    initialSessionData: {
      rssPalYouTubeTabs: [9],
    },
    sessionGet: async () => {
      getStarted.resolve();
      await releaseGet.promise;
      return { rssPalYouTubeTabs: [9, 101] };
    },
  });
  const resolver = createResolver(fake);

  const cleanup = resolver.cleanupOrphans();
  await getStarted.promise;
  const resolution = resolver.handleMessage(
    resolveMessage('active-during-cleanup'),
    sender(),
  );
  await waitFor(
    () => fake.calls.sessionSet.some(
      (value) =>
        value.rssPalYouTubeTabs.length === 1 &&
        value.rssPalYouTubeTabs[0] === 101,
    ),
    'concurrent active tab persistence',
  );
  releaseGet.resolve();
  await cleanup;

  assert.equal(fake.calls.remove.includes(9), true);
  assert.equal(fake.calls.remove.includes(101), false);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, [101]);

  await waitFor(
    () => fake.events.onUpdated.listenerCount() === 1,
    'active tab load listener after cleanup',
  );
  fake.completeTab(101);
  await resolution;
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);
});

test('orphan cleanup rechecks active tabs before every sequential removal', async () => {
  const removingStoredTab = deferred();
  const releaseStoredTab = deferred();
  const fake = createFakeChrome({
    initialSessionData: {
      rssPalYouTubeTabs: [9, 101],
    },
    remove: async (tabId) => {
      if (tabId === 9) {
        removingStoredTab.resolve();
        await releaseStoredTab.promise;
      }
    },
  });
  const resolver = createResolver(fake);

  const cleanup = resolver.cleanupOrphans();
  await removingStoredTab.promise;
  const resolution = resolver.handleMessage(
    resolveMessage('active-before-next-remove'),
    sender(),
  );
  await waitFor(
    () => fake.calls.sessionSet.some(
      (value) =>
        value.rssPalYouTubeTabs.length === 1 &&
        value.rssPalYouTubeTabs[0] === 101,
    ),
    'tab 101 active session snapshot',
  );

  releaseStoredTab.resolve();
  await cleanup;

  assert.deepEqual(fake.calls.remove, [9]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, [101]);

  fake.completeTab(101);
  await resolution;
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);
});

test('serialized session writes retain every concurrently active tab', async () => {
  const releaseFirstWrite = deferred();
  const fake = createFakeChrome({
    sessionSet: async (_values, callNumber) => {
      if (callNumber === 1) {
        await releaseFirstWrite.promise;
      }
    },
  });
  const resolver = createResolver(fake, { maxConcurrent: 2 });

  const first = resolver.handleMessage(
    resolveMessage('persist-one', VIDEO_ID),
    sender(7),
  );
  const second = resolver.handleMessage(
    resolveMessage('persist-two', OTHER_VIDEO_ID),
    sender(8),
  );
  await waitFor(
    () => fake.calls.sessionSet.length === 1,
    'first blocked session write',
  );
  assert.deepEqual(fake.calls.sessionSet[0], {
    rssPalYouTubeTabs: [101, 102],
  });

  releaseFirstWrite.resolve();
  await waitFor(
    () => fake.calls.sessionSet.length >= 2,
    'second serialized session write',
  );
  assert.deepEqual(fake.calls.sessionSet[1], {
    rssPalYouTubeTabs: [101, 102],
  });

  await waitFor(
    () => fake.events.onUpdated.listenerCount() === 2,
    'concurrent persisted tab listeners',
  );
  fake.completeTab(101);
  fake.completeTab(102);
  await Promise.all([first, second]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);
});

test('session persistence failure still closes and forgets the tab', async () => {
  let failFirstWrite = true;
  const fake = createFakeChrome({
    sessionSet: async () => {
      if (failFirstWrite) {
        failFirstWrite = false;
        throw new Error('private storage failure');
      }
    },
  });
  const resolver = createResolver(fake);

  assert.deepEqual(
    await resolver.handleMessage(
      resolveMessage('storage-failure'),
      sender(),
    ),
    { ok: false, code: 'LOCAL_NETWORK_ERROR' },
  );
  assert.deepEqual(fake.calls.remove, [101]);
  assert.deepEqual(fake.sessionData.rssPalYouTubeTabs, []);

  const retry = resolver.handleMessage(
    resolveMessage('storage-retry'),
    sender(),
  );
  await waitFor(
    () => fake.events.onUpdated.listenerCount() === 1,
    'retry after storage failure',
  );
  fake.completeTab(102);
  await retry;
  assert.equal(fake.calls.create.length, 2);
  assert.equal(fake.calls.remove.includes(102), true);
});

test('initial tabs.get complete state closes the load-listener race', async () => {
  const fake = createFakeChrome({
    create: async () => ({ id: 101, status: 'complete' }),
  });
  const resolver = createResolver(fake);

  assert.deepEqual(
    await resolver.handleMessage(
      resolveMessage('already-complete'),
      sender(),
    ),
    { ok: false, code: 'NO_SUPPORTED_FORMAT' },
  );
  assert.deepEqual(fake.calls.get, [101]);
  assert.equal(fake.calls.executeScript.length, 1);
  assert.equal(fake.events.onUpdated.listenerCount(), 0);
  assert.equal(fake.events.onRemoved.listenerCount(), 0);
});

test('maps tab create, lookup, and external removal failures to LOCAL_NETWORK_ERROR', async (t) => {
  await t.test('tabs.create rejection', async () => {
    const fake = createFakeChrome({
      create: async () => {
        throw new Error(
          'secret create failure https://www.youtube.com/watch?v=private',
        );
      },
    });
    const resolver = createResolver(fake);

    const result = await resolver.handleMessage(
      resolveMessage('create-rejection'),
      sender(),
    );

    assert.deepEqual(result, {
      ok: false,
      code: 'LOCAL_NETWORK_ERROR',
    });
    assert.equal(JSON.stringify(result).includes('private'), false);
    assert.deepEqual(fake.calls.executeScript, []);
  });

  await t.test('tabs.get rejection', async () => {
    const fake = createFakeChrome({
      get: async () => {
        throw new Error('secret lookup failure');
      },
    });
    const resolver = createResolver(fake, { tabTimeoutMs: 5 });

    assert.deepEqual(
      await resolver.handleMessage(
        resolveMessage('get-rejection'),
        sender(),
      ),
      { ok: false, code: 'LOCAL_NETWORK_ERROR' },
    );
    assert.equal(fake.events.onUpdated.listenerCount(), 0);
    assert.equal(fake.events.onRemoved.listenerCount(), 0);
  });

  await t.test('external tab removal', async () => {
    const fake = createFakeChrome();
    const resolver = createResolver(fake, { tabTimeoutMs: 1_000 });
    const resolution = resolver.handleMessage(
      resolveMessage('external-removal'),
      sender(),
    );
    await waitFor(
      () => fake.events.onRemoved.listenerCount() === 1,
      'external removal listener',
    );

    await fake.chromeApi.tabs.remove(101);

    assert.deepEqual(await resolution, {
      ok: false,
      code: 'LOCAL_NETWORK_ERROR',
    });
    assert.deepEqual(fake.calls.executeScript, []);
    assert.equal(fake.events.onUpdated.listenerCount(), 0);
    assert.equal(fake.events.onRemoved.listenerCount(), 0);
  });
});

test('sanitizes selection exceptions as INTERNAL_ERROR', async () => {
  const fake = createFakeChrome();
  const resolver = createResolver(fake, {
    selectPlayback() {
      throw new Error(
        'selection secret https://rr1---sn.example.googlevideo.com/private',
      );
    },
  });

  const result = await resolveAfterCompletingTab(
    fake,
    resolver,
    'selection-exception',
  );

  assert.deepEqual(result, {
    ok: false,
    code: 'INTERNAL_ERROR',
  });
  assert.equal(JSON.stringify(result).includes('googlevideo'), false);
  assert.equal(JSON.stringify(result).includes('selection secret'), false);
});

test('background statically wires YouTube dependencies and lifecycle first', () => {
  const source = fs.readFileSync(
    path.join(__dirname, '..', 'background.js'),
    'utf8',
  );
  const dependencyOrder = [
    "'queue.js'",
    "'youtube/protocol.js'",
    "'youtube/format-selection.js'",
    "'youtube/page-capture.js'",
    "'youtube/resolver.js'",
  ];
  let previousIndex = source.indexOf('importScripts(');
  assert.notEqual(previousIndex, -1);
  for (const dependency of dependencyOrder) {
    const dependencyIndex = source.indexOf(dependency, previousIndex);
    assert.notEqual(
      dependencyIndex,
      -1,
      `missing background dependency ${dependency}`,
    );
    assert.equal(dependencyIndex > previousIndex, true);
    previousIndex = dependencyIndex;
  }

  const constructorIndex = source.indexOf(
    'globalThis.__rssPalCreateYouTubeResolver({',
  );
  assert.equal(constructorIndex > previousIndex, true);
  assert.match(source, /chromeApi:\s*chrome/);
  assert.match(
    source,
    /protocol:\s*globalThis\.__rssPalYouTubeProtocol/,
  );
  assert.match(
    source,
    /selectPlayback:\s*globalThis\.__rssPalYouTubeFormatSelection\.selectPlayback/,
  );
  assert.match(
    source,
    /capturePageState:\s*globalThis\.__rssPalCaptureYouTubePageState/,
  );

  const messageListener = source.indexOf(
    'chrome.runtime.onMessage.addListener',
  );
  const youtubeMessageBranch = source.indexOf(
    'globalThis.__rssPalYouTubeProtocol.RUNTIME_RESOLVE',
    messageListener,
  );
  const flushBranch = source.indexOf(
    "msg.action === 'flushQueue'",
    messageListener,
  );
  const syncBranch = source.indexOf(
    "msg.action === 'startSync'",
    messageListener,
  );
  const badgeBranch = source.indexOf(
    "msg.action === 'updateBadge'",
    messageListener,
  );
  assert.equal(
    messageListener < youtubeMessageBranch &&
      youtubeMessageBranch < flushBranch &&
      flushBranch < syncBranch &&
      syncBranch < badgeBranch,
    true,
  );
  assert.match(
    source,
    /chrome\.tabs\.onRemoved\.addListener\([\s\S]*cancelRequestsForTab/,
  );
  assert.match(
    source,
    /chrome\.runtime\.onInstalled\.addListener\([\s\S]*scheduleAlarms\(\)[\s\S]*cleanupYouTubeOrphans\(\)/,
  );
  assert.match(
    source,
    /chrome\.runtime\.onStartup\.addListener\([\s\S]*scheduleAlarms\(\)[\s\S]*cleanupYouTubeOrphans\(\)/,
  );
  const cleanupCalls =
    source.match(/cleanupYouTubeOrphans\(\);/g) || [];
  assert.equal(cleanupCalls.length >= 3, true);
});

test('background routes YouTube messages and cleanup through the resolver', async () => {
  const source = fs.readFileSync(
    path.join(__dirname, '..', 'background.js'),
    'utf8',
  );
  const imported = [];
  const alarmsCreated = [];
  const cleanupCalls = [];
  const cancellationCalls = [];
  const messageCalls = [];
  const events = {
    alarm: createEvent(),
    installed: createEvent(),
    message: createEvent(),
    startup: createEvent(),
    tabRemoved: createEvent(),
  };
  const resolver = {
    async cleanupOrphans() {
      cleanupCalls.push('cleanup');
    },
    cancelRequestsForTab(tabId) {
      cancellationCalls.push(tabId);
      return Promise.resolve();
    },
    handleMessage(message, messageSender) {
      messageCalls.push({ message, sender: messageSender });
      return Promise.resolve({ ok: false, code: 'LOGIN_REQUIRED' });
    },
  };
  let constructedWith = null;
  const chromeApi = {
    action: {
      setBadgeBackgroundColor() {},
      setBadgeText() {},
    },
    alarms: {
      create(name, details) {
        alarmsCreated.push({ name, details });
      },
      onAlarm: events.alarm,
    },
    runtime: {
      lastError: null,
      onInstalled: events.installed,
      onMessage: events.message,
      onStartup: events.startup,
    },
    scripting: {
      async executeScript() {
        return [];
      },
    },
    storage: {
      local: {
        async get() {
          return {};
        },
        async set() {},
      },
      sync: {
        async get() {
          return {};
        },
      },
    },
    tabs: {
      async create() {
        return { id: 1 };
      },
      async get() {
        return { status: 'complete' };
      },
      onRemoved: events.tabRemoved,
      onUpdated: createEvent(),
      async remove() {},
    },
  };
  const protocolGlobal = {
    RUNTIME_RESOLVE: protocol.RUNTIME_RESOLVE,
    RUNTIME_CANCEL: protocol.RUNTIME_CANCEL,
  };
  const selectionGlobal = { selectPlayback() {} };
  const captureGlobal = async function captureGlobal() {};
  const context = vm.createContext({
    URL,
    chrome: chromeApi,
    clearTimeout,
    console,
    fetch,
    globalThis: null,
    importScripts(...scripts) {
      imported.push(...scripts);
    },
    setTimeout,
  });
  context.globalThis = context;
  context.__rssPalYouTubeProtocol = protocolGlobal;
  context.__rssPalYouTubeFormatSelection = selectionGlobal;
  context.__rssPalCaptureYouTubePageState = captureGlobal;
  context.__rssPalCreateYouTubeResolver = (options) => {
    constructedWith = options;
    return resolver;
  };

  vm.runInContext(source, context);
  await new Promise((resolve) => setImmediate(resolve));

  assert.deepEqual(imported, [
    'queue.js',
    'youtube/protocol.js',
    'youtube/format-selection.js',
    'youtube/page-capture.js',
    'youtube/resolver.js',
  ]);
  assert.equal(constructedWith.chromeApi, chromeApi);
  assert.equal(constructedWith.protocol, protocolGlobal);
  assert.equal(
    constructedWith.selectPlayback,
    selectionGlobal.selectPlayback,
  );
  assert.equal(constructedWith.capturePageState, captureGlobal);
  assert.equal(cleanupCalls.length, 1);

  events.installed.emit();
  events.startup.emit();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(cleanupCalls.length, 3);
  assert.equal(alarmsCreated.length, 6);

  events.tabRemoved.emit(44);
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(cancellationCalls, [44]);

  const response = deferred();
  const runtimeMessage = resolveMessage('background-route');
  const runtimeSender = sender();
  const listenerResults = events.message.emit(
    runtimeMessage,
    runtimeSender,
    response.resolve,
  );
  assert.equal(listenerResults[0], true);
  assert.deepEqual(await response.promise, {
    ok: false,
    code: 'LOGIN_REQUIRED',
  });
  assert.deepEqual(messageCalls, [{
    message: runtimeMessage,
    sender: runtimeSender,
  }]);

  resolver.handleMessage = () => Promise.reject(
    new Error(
      'private resolver failure https://www.youtube.com/watch?v=secret',
    ),
  );
  const rejectedResponse = deferred();
  events.message.emit(
    resolveMessage('background-rejection'),
    runtimeSender,
    rejectedResponse.resolve,
  );
  assert.deepEqual(
    JSON.parse(JSON.stringify(await rejectedResponse.promise)),
    { ok: false, code: 'INTERNAL_ERROR' },
  );

  resolver.handleMessage = () => Promise.resolve({ ok: true });
  events.message.emit(
    resolveMessage('closed-response-port'),
    runtimeSender,
    () => {
      throw new Error('message port closed with private details');
    },
  );
  await new Promise((resolve) => setImmediate(resolve));
});
