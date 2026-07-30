# YouTube Logged-in Browser Bridge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Play YouTube videos inside the Chrome version of RSS Pal through the user's logged-in YouTube session and local Clash path, preferring 1080p with a 720p fallback and sending no media through Beijing or OCI.

**Architecture:** A production-origin content script forwards one validated resolve request to the RSS Pal MV3 service worker. The worker opens an inactive YouTube tab, injects a fixed MAIN-world capture function, joins actual GoogleVideo request URLs to sanitized player metadata, closes the tab, and returns an in-memory playback contract; the frontend builds a local DASH MPD or uses a progressive fallback, while a narrowly scoped extension rule supplies CORS headers only for RSS Pal-initiated GoogleVideo range requests.

**Tech Stack:** Chrome Manifest V3, classic service-worker `importScripts`, `chrome.tabs`, `chrome.scripting`, `chrome.storage.session`, Declarative Net Request, JavaScript `node:test`, React 18, TypeScript, Vitest, dash.js 5, Docker Compose, Nginx.

---

## Scope and File Map

The extension and frontend are one plan rather than independent sub-projects:
neither side can produce working playback without the shared message and media
contract.

### Extension files

- Create `extension/youtube/protocol.js`: shared constants and pure validation
  for page and runtime messages.
- Create `extension/youtube/protocol.test.js`: request, sender, and identifier
  validation.
- Create `extension/youtube/bridge-content.js`: exact-origin page-to-extension
  bridge.
- Create `extension/youtube/bridge-content.test.js`: ready handshake, resolve,
  cancellation, origin, and correlation coverage.
- Create `extension/youtube/format-selection.js`: sanitize format metadata,
  match finalized URLs by `itag`, select adaptive/progressive playback, and
  derive expiry.
- Create `extension/youtube/format-selection.test.js`: 1080p preference, 720p
  fallback, progressive fallback, ciphered-entry matching, and error coverage.
- Create `extension/youtube/page-capture.js`: standalone MAIN-world YouTube
  player/resource capture function.
- Create `extension/youtube/page-capture.test.js`: fake-player tests for
  quality forcing, resource capture, sanitization, and player shutdown.
- Create `extension/youtube/resolver.js`: tab lifecycle, deduplication,
  cancellation, orphan cleanup, execution, and public error mapping.
- Create `extension/youtube/resolver.test.js`: fake-Chrome lifecycle coverage.
- Create `extension/youtube/manifest.test.js`: permission, origin, ruleset, and
  version regression tests.
- Create `extension/rules/youtube-media-cors.json`: restricted GoogleVideo CORS
  response rule.
- Modify `extension/background.js`: load and route the YouTube resolver without
  changing queue/sync behavior.
- Modify `extension/manifest.json`: version `1.8.4`, permission, ruleset, and
  production content script.
- Modify `extension/package.json`: version `1.8.4` and repeatable `test` /
  `check` scripts.

### Frontend files

- Create `frontend/src/youtube/bridge.ts`: typed extension detection, version
  checking, resolution, validation, timeout, and cancellation.
- Create `frontend/test/youtubeBridge.test.ts`: same-window/origin,
  correlation, old-version, timeout, and cancellation tests.
- Create `frontend/src/youtube/mpd.ts`: escaped static VOD MPD generation.
- Create `frontend/test/youtubeMpd.test.ts`: adaptive metadata and XML safety
  coverage.
- Create `frontend/src/components/YouTubeBrowserPlayer.tsx`: explicit-click
  resolver and local DASH/progressive player state machine.
- Create `frontend/test/YouTubeBrowserPlayer.test.tsx`: browser bridge, dash.js,
  fallback, error, retry, and teardown coverage.
- Modify `frontend/src/components/ArticlePlayerCard.tsx`: route primary YouTube
  media to the browser player.
- Modify `frontend/src/components/VideoEmbed.tsx`: route inline YouTube
  placeholders to the same browser player and leave Bilibili unchanged.
- Modify `frontend/test/ArticlePlayerCardYouTube.test.tsx`: assert browser
  routing without backend relay.
- Modify `frontend/test/VideoEmbed.test.tsx`: assert browser routing without a
  YouTube iframe.

No backend, database, Nginx, OCI, DNS, Pake, or server-relay file changes belong
in this implementation.

## Task 1: Lock the Extension Message Protocol

**Files:**
- Create: `extension/youtube/protocol.test.js`
- Create: `extension/youtube/protocol.js`

- [ ] **Step 1: Write the failing protocol tests**

Create `extension/youtube/protocol.test.js`:

```js
const test = require('node:test');
const assert = require('node:assert/strict');

const protocol = require('./protocol.js');

test('accepts one bounded YouTube resolve request', () => {
  assert.deepEqual(protocol.validateRuntimeResolve({
    action: protocol.RUNTIME_RESOLVE,
    requestId: 'req_01HX9X2M7T',
    videoId: 'dQw4w9WgXcQ',
  }), {
    requestId: 'req_01HX9X2M7T',
    videoId: 'dQw4w9WgXcQ',
  });
});

test('rejects URLs, malformed IDs, and unsafe request IDs', () => {
  assert.equal(protocol.validateRuntimeResolve({
    action: protocol.RUNTIME_RESOLVE,
    requestId: 'req with spaces',
    videoId: 'dQw4w9WgXcQ',
  }), null);
  assert.equal(protocol.validateRuntimeResolve({
    action: protocol.RUNTIME_RESOLVE,
    requestId: 'req_1',
    videoId: 'https://youtube.com/watch?v=dQw4w9WgXcQ',
  }), null);
  assert.equal(protocol.validateRuntimeResolve({
    action: protocol.RUNTIME_RESOLVE,
    requestId: 'req_1',
    videoId: 'dQw4w9WgXcQ',
    url: 'https://example.com',
  }), null);
});

test('accepts only the production RSS Pal sender', () => {
  assert.equal(protocol.isTrustedSender({
    tab: { id: 8, url: 'https://rss.morefreeze.top/articles/2401' },
  }), true);
  assert.equal(protocol.isTrustedSender({
    tab: { id: 8, url: 'https://evil.example/articles/2401' },
  }), false);
  assert.equal(protocol.isTrustedSender({
    url: 'https://rss.morefreeze.top/articles/2401',
  }), false);
});
```

- [ ] **Step 2: Run the protocol tests and verify RED**

Run:

```bash
cd extension
node --test youtube/protocol.test.js
```

Expected: FAIL with `Cannot find module './protocol.js'`.

- [ ] **Step 3: Implement the shared protocol**

Create `extension/youtube/protocol.js`:

```js
(function installProtocol(root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  } else {
    root.__rssPalYouTubeProtocol = api;
  }
})(typeof globalThis === 'object' ? globalThis : this, function createProtocol() {
  const RSS_ORIGIN = 'https://rss.morefreeze.top';
  const PAGE_PING = 'RSS_PAL_YOUTUBE_BRIDGE_PING';
  const PAGE_READY = 'RSS_PAL_YOUTUBE_BRIDGE_READY';
  const PAGE_RESOLVE = 'RSS_PAL_YOUTUBE_RESOLVE_REQUEST';
  const PAGE_CANCEL = 'RSS_PAL_YOUTUBE_RESOLVE_CANCEL';
  const PAGE_RESPONSE = 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE';
  const RUNTIME_RESOLVE = 'rssPalYouTubeResolve';
  const RUNTIME_CANCEL = 'rssPalYouTubeCancel';
  const VIDEO_ID_RE = /^[A-Za-z0-9_-]{11}$/;
  const REQUEST_ID_RE = /^[A-Za-z0-9_-]{1,80}$/;

  function isVideoId(value) {
    return typeof value === 'string' && VIDEO_ID_RE.test(value);
  }

  function isRequestId(value) {
    return typeof value === 'string' && REQUEST_ID_RE.test(value);
  }

  function validateRuntimeResolve(message) {
    if (!message || message.action !== RUNTIME_RESOLVE) return null;
    const keys = Object.keys(message).sort();
    if (keys.join(',') !== 'action,requestId,videoId') return null;
    if (!isRequestId(message.requestId) || !isVideoId(message.videoId)) return null;
    return { requestId: message.requestId, videoId: message.videoId };
  }

  function validateRuntimeCancel(message) {
    if (!message || message.action !== RUNTIME_CANCEL) return null;
    const keys = Object.keys(message).sort();
    if (keys.join(',') !== 'action,requestId') return null;
    if (!isRequestId(message.requestId)) return null;
    return { requestId: message.requestId };
  }

  function isTrustedSender(sender) {
    if (!sender || !sender.tab || !Number.isInteger(sender.tab.id)) return false;
    try {
      const url = new URL(sender.tab.url);
      return url.origin === RSS_ORIGIN;
    } catch (_) {
      return false;
    }
  }

  return Object.freeze({
    RSS_ORIGIN,
    PAGE_PING,
    PAGE_READY,
    PAGE_RESOLVE,
    PAGE_CANCEL,
    PAGE_RESPONSE,
    RUNTIME_RESOLVE,
    RUNTIME_CANCEL,
    isVideoId,
    isRequestId,
    validateRuntimeResolve,
    validateRuntimeCancel,
    isTrustedSender,
  });
});
```

- [ ] **Step 4: Run the protocol tests and verify GREEN**

Run:

```bash
cd extension
node --test youtube/protocol.test.js
```

Expected: 3 tests pass.

- [ ] **Step 5: Commit Task 1**

```bash
git add extension/youtube/protocol.js extension/youtube/protocol.test.js
git commit -m "feat: define YouTube extension protocol"
```

## Task 2: Add the Exact-Origin Content Bridge

**Files:**
- Create: `extension/youtube/bridge-content.test.js`
- Create: `extension/youtube/bridge-content.js`

- [ ] **Step 1: Write failing bridge tests**

Create `extension/youtube/bridge-content.test.js` with a fake page and runtime:

```js
const test = require('node:test');
const assert = require('node:assert/strict');

const protocol = require('./protocol.js');
const { createYouTubeContentBridge } = require('./bridge-content.js');

function createHarness() {
  const listeners = new Set();
  const posts = [];
  const runtimeMessages = [];
  const page = {
    location: { origin: protocol.RSS_ORIGIN },
    addEventListener(type, listener) {
      if (type === 'message') listeners.add(listener);
    },
    removeEventListener(type, listener) {
      if (type === 'message') listeners.delete(listener);
    },
    postMessage(data, targetOrigin) {
      posts.push({ data, targetOrigin });
    },
  };
  const runtime = {
    getManifest: () => ({ version: '1.8.4' }),
    async sendMessage(message) {
      runtimeMessages.push(message);
      return message.action === protocol.RUNTIME_RESOLVE
        ? { ok: true, playback: { mode: 'progressive', quality: 720 } }
        : { ok: true };
    },
  };
  function dispatch(data, origin = protocol.RSS_ORIGIN, source = page) {
    for (const listener of listeners) listener({ data, origin, source });
  }
  return { page, runtime, posts, runtimeMessages, dispatch };
}

test('answers ping with bridge version at the exact origin', () => {
  const h = createHarness();
  createYouTubeContentBridge(h.page, h.runtime, protocol);
  h.dispatch({ type: protocol.PAGE_PING });
  assert.deepEqual(h.posts.at(-1), {
    data: { type: protocol.PAGE_READY, version: '1.8.4' },
    targetOrigin: protocol.RSS_ORIGIN,
  });
});

test('forwards a correlated resolve request and response', async () => {
  const h = createHarness();
  createYouTubeContentBridge(h.page, h.runtime, protocol);
  h.dispatch({
    type: protocol.PAGE_RESOLVE,
    requestId: 'req_1',
    videoId: 'dQw4w9WgXcQ',
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(h.runtimeMessages[0], {
    action: protocol.RUNTIME_RESOLVE,
    requestId: 'req_1',
    videoId: 'dQw4w9WgXcQ',
  });
  assert.equal(h.posts.at(-1).data.requestId, 'req_1');
  assert.equal(h.posts.at(-1).data.ok, true);
});

test('ignores another origin and forwards cancellation', async () => {
  const h = createHarness();
  createYouTubeContentBridge(h.page, h.runtime, protocol);
  h.dispatch({
    type: protocol.PAGE_RESOLVE,
    requestId: 'req_1',
    videoId: 'dQw4w9WgXcQ',
  }, 'https://evil.example');
  assert.equal(h.runtimeMessages.length, 0);
  h.dispatch({ type: protocol.PAGE_CANCEL, requestId: 'req_1' });
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(h.runtimeMessages[0], {
    action: protocol.RUNTIME_CANCEL,
    requestId: 'req_1',
  });
});
```

- [ ] **Step 2: Run the bridge tests and verify RED**

Run:

```bash
cd extension
node --test youtube/bridge-content.test.js
```

Expected: FAIL with `Cannot find module './bridge-content.js'`.

- [ ] **Step 3: Implement the content bridge**

Create `extension/youtube/bridge-content.js`:

```js
(function installBridge(root) {
  function createYouTubeContentBridge(page, runtime, protocol) {
    function post(data) {
      page.postMessage(data, protocol.RSS_ORIGIN);
    }

    async function onMessage(event) {
      if (event.source !== page || event.origin !== protocol.RSS_ORIGIN) return;
      const message = event.data;
      if (!message || typeof message !== 'object') return;

      if (message.type === protocol.PAGE_PING) {
        post({
          type: protocol.PAGE_READY,
          version: runtime.getManifest().version,
        });
        return;
      }

      if (message.type === protocol.PAGE_CANCEL && protocol.isRequestId(message.requestId)) {
        try {
          await runtime.sendMessage({
            action: protocol.RUNTIME_CANCEL,
            requestId: message.requestId,
          });
        } catch (_) {
          return;
        }
        return;
      }

      if (message.type !== protocol.PAGE_RESOLVE) return;
      if (!protocol.isRequestId(message.requestId) || !protocol.isVideoId(message.videoId)) return;

      let response;
      try {
        response = await runtime.sendMessage({
          action: protocol.RUNTIME_RESOLVE,
          requestId: message.requestId,
          videoId: message.videoId,
        });
      } catch (_) {
        response = { ok: false, code: 'INTERNAL_ERROR' };
      }
      post({
        ...response,
        type: protocol.PAGE_RESPONSE,
        requestId: message.requestId,
      });
    }

    page.addEventListener('message', onMessage);
    post({ type: protocol.PAGE_READY, version: runtime.getManifest().version });
    return () => page.removeEventListener('message', onMessage);
  }

  if (typeof module === 'object' && module.exports) {
    module.exports = { createYouTubeContentBridge };
    return;
  }
  createYouTubeContentBridge(
    window,
    chrome.runtime,
    root.__rssPalYouTubeProtocol,
  );
})(typeof globalThis === 'object' ? globalThis : this);
```

- [ ] **Step 4: Run both bridge/protocol tests**

Run:

```bash
cd extension
node --test youtube/protocol.test.js youtube/bridge-content.test.js
```

Expected: 6 tests pass.

- [ ] **Step 5: Commit Task 2**

```bash
git add extension/youtube/bridge-content.js extension/youtube/bridge-content.test.js
git commit -m "feat: bridge RSS Pal page to extension"
```

## Task 3: Select Safe, Finalized Media Tracks

**Files:**
- Create: `extension/youtube/format-selection.test.js`
- Create: `extension/youtube/format-selection.js`

- [ ] **Step 1: Write failing deterministic-selection tests**

Create fixtures in `extension/youtube/format-selection.test.js` that use
realistic sanitized player fields:

```js
const test = require('node:test');
const assert = require('node:assert/strict');

const {
  normalizeGoogleVideoUrl,
  parseItag,
  selectPlayback,
} = require('./format-selection.js');

function video(itag, height, fps, url) {
  return {
    itag,
    url,
    mimeType: 'video/mp4; codecs="avc1.640028"',
    bitrate: height === 1080 ? 3500000 : 1800000,
    width: height === 1080 ? 1920 : 1280,
    height,
    fps,
    approxDurationMs: '120000',
    initRange: { start: '0', end: '739' },
    indexRange: { start: '740', end: '1251' },
  };
}

function audio(itag, url) {
  return {
    itag,
    url,
    mimeType: 'audio/mp4; codecs="mp4a.40.2"',
    bitrate: 128000,
    audioQuality: 'AUDIO_QUALITY_MEDIUM',
    audioSampleRate: '44100',
    audioChannels: 2,
    approxDurationMs: '120000',
    initRange: { start: '0', end: '721' },
    indexRange: { start: '722', end: '1197' },
  };
}

test('parses itag from query and path forms', () => {
  assert.equal(parseItag('https://r1.googlevideo.com/videoplayback?itag=137&expire=2000000000'), 137);
  assert.equal(parseItag('https://r1.googlevideo.com/videoplayback/itag/140/expire/2000000000'), 140);
  assert.equal(parseItag('https://example.com/file'), null);
});

test('removes request-local range parameters but preserves signatures and PO tokens', () => {
  assert.equal(
    normalizeGoogleVideoUrl(
      'https://r1.googlevideo.com/videoplayback?itag=137&range=0-1048575&rn=3&rbuf=12&sig=abc&pot=xyz',
    ),
    'https://r1.googlevideo.com/videoplayback?itag=137&sig=abc&pot=xyz',
  );
});

test('prefers 1080p 30fps adaptive playback and observed URLs', () => {
  const formats = [
    video(137, 1080, 30, 'https://r1.googlevideo.com/videoplayback?itag=137&expire=2000000000&old=1'),
    video(399, 1080, 60, 'https://r1.googlevideo.com/videoplayback?itag=399&expire=2000000000'),
    video(136, 720, 30, 'https://r1.googlevideo.com/videoplayback?itag=136&expire=2000000000'),
    audio(140, 'https://r1.googlevideo.com/videoplayback?itag=140&expire=2000000000'),
  ];
  const playback = selectPlayback({
    status: 'OK',
    formats,
    resourceUrls: [
      'https://r1.googlevideo.com/videoplayback?itag=137&expire=2000000000&observed=1',
      'https://r1.googlevideo.com/videoplayback?itag=140&expire=2000000000&observed=1',
    ],
  }, 1700000000000);
  assert.equal(playback.ok, true);
  assert.equal(playback.playback.mode, 'dash');
  assert.equal(playback.playback.quality, 1080);
  assert.match(playback.playback.video.url, /observed=1/);
});

test('falls back to 720p adaptive and then progressive playback', () => {
  const adaptive = selectPlayback({
    status: 'OK',
    formats: [
      video(136, 720, 30, 'https://r1.googlevideo.com/videoplayback?itag=136&expire=2000000000'),
      audio(140, 'https://r1.googlevideo.com/videoplayback?itag=140&expire=2000000000'),
    ],
    resourceUrls: [],
  }, 1700000000000);
  assert.equal(adaptive.playback.quality, 720);

  const progressive = selectPlayback({
    status: 'OK',
    formats: [{
      ...video(22, 720, 30, 'https://r1.googlevideo.com/videoplayback?itag=22&expire=2000000000'),
      audioQuality: 'AUDIO_QUALITY_MEDIUM',
    }],
    resourceUrls: [],
  }, 1700000000000);
  assert.equal(progressive.playback.mode, 'progressive');
  assert.equal(progressive.playback.quality, 720);
});

test('maps login and missing-format failures without leaking reason text', () => {
  assert.deepEqual(selectPlayback({
    status: 'LOGIN_REQUIRED',
    reason: 'account detail must not cross the bridge',
    formats: [],
    resourceUrls: [],
  }, 1700000000000), { ok: false, code: 'LOGIN_REQUIRED' });
  assert.deepEqual(selectPlayback({
    status: 'OK',
    formats: [],
    resourceUrls: [],
  }, 1700000000000), { ok: false, code: 'NO_SUPPORTED_FORMAT' });
});
```

- [ ] **Step 2: Run the selection tests and verify RED**

Run:

```bash
cd extension
node --test youtube/format-selection.test.js
```

Expected: FAIL with `Cannot find module './format-selection.js'`.

- [ ] **Step 3: Implement sanitization, URL matching, and selection**

Create `extension/youtube/format-selection.js` as a UMD-style pure module. In
Node it exports the object; in Chrome it attaches the same object as
`globalThis.__rssPalYouTubeFormatSelection`. Its exported contract must be:

```js
{
  normalizeGoogleVideoUrl,
  parseItag,
  sanitizeFormat,
  selectPlayback,
}
```

Implement the selection with these exact helpers:

```js
function parseItag(rawUrl) {
  try {
    const url = new URL(rawUrl);
    const queryItag = url.searchParams.get('itag');
    if (queryItag && /^\d+$/.test(queryItag)) return Number(queryItag);
    const pathMatch = url.pathname.match(/\/itag\/(\d+)(?:\/|$)/);
    return pathMatch ? Number(pathMatch[1]) : null;
  } catch (_) {
    return null;
  }
}

function isGoogleVideoUrl(rawUrl) {
  try {
    const url = new URL(rawUrl);
    return url.protocol === 'https:' &&
      (url.hostname === 'googlevideo.com' || url.hostname.endsWith('.googlevideo.com')) &&
      url.pathname.includes('/videoplayback');
  } catch (_) {
    return false;
  }
}

function normalizeGoogleVideoUrl(rawUrl) {
  if (!isGoogleVideoUrl(rawUrl)) return null;
  const url = new URL(rawUrl);
  for (const key of ['range', 'rn', 'rbuf', 'ump', 'alr']) {
    url.searchParams.delete(key);
  }
  return url.toString();
}

function numericRange(value) {
  if (!value || !/^\d+$/.test(value.start) || !/^\d+$/.test(value.end)) return null;
  const start = Number(value.start);
  const end = Number(value.end);
  if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end) || end < start) return null;
  return { start, end };
}

function splitMimeType(value) {
  const match = String(value || '').match(/^([^;]+)(?:;\s*codecs="([^"]+)")?/);
  return match ? { mimeType: match[1], codecs: match[2] || '' } : null;
}

function sanitizeFormat(format) {
  if (!format || !Number.isInteger(Number(format.itag))) return null;
  const mime = splitMimeType(format.mimeType);
  if (!mime) return null;
  const out = {
    itag: Number(format.itag),
    mimeType: mime.mimeType,
    codecs: mime.codecs,
    bitrate: Number(format.bitrate) || 0,
    durationMs: Number(format.approxDurationMs) || 0,
  };
  if (isGoogleVideoUrl(format.url)) out.url = format.url;
  if (Number(format.width) > 0) out.width = Number(format.width);
  if (Number(format.height) > 0) out.height = Number(format.height);
  if (Number(format.fps) > 0) out.frameRate = Number(format.fps);
  if (Number(format.audioSampleRate) > 0) out.audioSampleRate = Number(format.audioSampleRate);
  if (Number(format.audioChannels) > 0) out.audioChannels = Number(format.audioChannels);
  if (format.audioQuality) out.hasAudio = true;
  const initRange = numericRange(format.initRange);
  const indexRange = numericRange(format.indexRange);
  if (initRange) out.initRange = initRange;
  if (indexRange) out.indexRange = indexRange;
  return out;
}
```

Build an `itag -> URL` map from sanitized direct URLs, then overwrite it with
normalized, validated `resourceUrls` because observed requests are the source
of truth. Normalization removes only request-local `range`, `rn`, `rbuf`,
`ump`, and `alr`; it must retain `sig`, `lsig`, `n`, `pot`, and every other
authorization field. Choose adaptive video formats with
`720 <= height <= 1080`, initialization and index ranges, and no audio; sort by
height descending, `fps <= 30` first, `video/mp4` first, then bitrate
descending. Choose audio formats with ranges, `audio/mp4` first, then bitrate
descending. Choose progressive formats with audio and `height <= 720`, height
descending.

Return adaptive tracks with the exact numeric fields defined in the design.
Derive `expiresAt` from the earliest positive `expire` query value among
selected URLs; if the URL has no expiry, use `new Date(nowMs + 300000)`. Map
`LOGIN_REQUIRED` to `LOGIN_REQUIRED`, non-`OK` statuses to
`VIDEO_UNAVAILABLE`, and an empty selection to `NO_SUPPORTED_FORMAT`.

- [ ] **Step 4: Run the selection tests and verify GREEN**

Run:

```bash
cd extension
node --test youtube/format-selection.test.js
```

Expected: 4 tests pass.

- [ ] **Step 5: Commit Task 3**

```bash
git add extension/youtube/format-selection.js extension/youtube/format-selection.test.js
git commit -m "feat: select browser YouTube formats"
```

## Task 4: Capture the Real YouTube Player Requests

**Files:**
- Create: `extension/youtube/page-capture.test.js`
- Create: `extension/youtube/page-capture.js`

- [ ] **Step 1: Write failing fake-player tests**

Create `extension/youtube/page-capture.test.js` with a fake MAIN-world
environment. The success test must provide:

```js
const playerResponse = {
  playabilityStatus: { status: 'OK' },
  streamingData: {
    adaptiveFormats: [
      {
        itag: 137,
        mimeType: 'video/mp4; codecs="avc1.640028"',
        bitrate: 3500000,
        width: 1920,
        height: 1080,
        fps: 30,
        approxDurationMs: '120000',
        initRange: { start: '0', end: '739' },
        indexRange: { start: '740', end: '1251' },
        signatureCipher: 'private-cipher-data',
      },
      {
        itag: 140,
        mimeType: 'audio/mp4; codecs="mp4a.40.2"',
        bitrate: 128000,
        audioQuality: 'AUDIO_QUALITY_MEDIUM',
        audioSampleRate: '44100',
        audioChannels: 2,
        approxDurationMs: '120000',
        initRange: { start: '0', end: '721' },
        indexRange: { start: '722', end: '1197' },
      },
    ],
    formats: [],
  },
};
```

The fake player records `mute`, `setPlaybackQualityRange('hd1080',
'hd1080')`, `playVideo`, and `pauseVideo`. The fake performance API returns
two finalized GoogleVideo resource URLs after `playVideo`. Assert the result:

- has status `OK`;
- contains only sanitized format fields and no `signatureCipher`;
- contains both finalized resource URLs;
- pauses the player before returning.

Add a second test with `playabilityStatus.status = 'LOGIN_REQUIRED'` and assert
that no playback method runs and no reason/account detail is copied.

- [ ] **Step 2: Run the page-capture tests and verify RED**

Run:

```bash
cd extension
node --test youtube/page-capture.test.js
```

Expected: FAIL with `Cannot find module './page-capture.js'`.

- [ ] **Step 3: Implement one serializable capture function**

Create `extension/youtube/page-capture.js`. It must export and attach one
standalone function:

```js
async function captureYouTubePageState(options, injectedEnvironment) {
  const env = injectedEnvironment || {
    document,
    performance,
    root: window,
    sleep: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
  };
  const timeoutMs = Math.min(Math.max(Number(options && options.timeoutMs) || 15000, 1000), 20000);
  const deadline = Date.now() + timeoutMs;
  let player = null;

  while (Date.now() < deadline) {
    player = env.document.getElementById('movie_player');
    if (player && typeof player.getPlayerResponse === 'function') break;
    await env.sleep(100);
  }
  if (!player || typeof player.getPlayerResponse !== 'function') {
    return { status: 'CAPTURE_TIMEOUT', formats: [], resourceUrls: [] };
  }

  const response = player.getPlayerResponse() || env.root.ytInitialPlayerResponse || {};
  const playability = response.playabilityStatus || {};
  if (playability.status !== 'OK') {
    return {
      status: playability.status || 'UNPLAYABLE',
      formats: [],
      resourceUrls: [],
    };
  }

  function sanitized(format) {
    if (!format || typeof format !== 'object') return null;
    const out = {
      itag: format.itag,
      mimeType: format.mimeType,
      bitrate: format.bitrate,
      width: format.width,
      height: format.height,
      fps: format.fps,
      approxDurationMs: format.approxDurationMs,
      initRange: format.initRange,
      indexRange: format.indexRange,
      audioQuality: format.audioQuality,
      audioSampleRate: format.audioSampleRate,
      audioChannels: format.audioChannels,
    };
    if (typeof format.url === 'string') out.url = format.url;
    return out;
  }

  const streamingData = response.streamingData || {};
  const formats = []
    .concat(Array.isArray(streamingData.adaptiveFormats) ? streamingData.adaptiveFormats : [])
    .concat(Array.isArray(streamingData.formats) ? streamingData.formats : [])
    .map(sanitized)
    .filter(Boolean);

  function capturedUrls() {
    return env.performance.getEntriesByType('resource')
      .map((entry) => entry && entry.name)
      .filter((name) => {
        if (typeof name !== 'string') return false;
        try {
          const url = new URL(name);
          return (url.hostname === 'googlevideo.com' || url.hostname.endsWith('.googlevideo.com')) &&
            url.pathname.includes('/videoplayback');
        } catch (_) {
          return false;
        }
      });
  }

  try {
    const levels = typeof player.getAvailableQualityLevels === 'function'
      ? player.getAvailableQualityLevels()
      : [];
    const target = levels.includes('hd1080')
      ? 'hd1080'
      : levels.includes('hd720')
        ? 'hd720'
        : null;
    if (target && typeof player.setPlaybackQualityRange === 'function') {
      player.setPlaybackQualityRange(target, target);
    } else if (target && typeof player.setPlaybackQuality === 'function') {
      player.setPlaybackQuality(target);
    }
    if (typeof player.mute === 'function') player.mute();
    if (typeof player.playVideo === 'function') player.playVideo();

    let urls = capturedUrls();
    while (Date.now() < deadline) {
      const itags = new Set(urls.map((rawUrl) => {
        try {
          const url = new URL(rawUrl);
          return url.searchParams.get('itag') || (url.pathname.match(/\/itag\/(\d+)/) || [])[1];
        } catch (_) {
          return null;
        }
      }).filter(Boolean));
      if (itags.size >= 2 || (itags.size >= 1 && formats.some((format) => format.audioQuality))) break;
      await env.sleep(250);
      urls = capturedUrls();
    }
    return { status: 'OK', formats, resourceUrls: Array.from(new Set(urls)) };
  } finally {
    if (typeof player.pauseVideo === 'function') player.pauseVideo();
  }
}
```

Use a conditional CommonJS export for tests and otherwise attach the function
as `globalThis.__rssPalCaptureYouTubePageState`. The function passed to
`chrome.scripting.executeScript` must not reference any outer helper.

- [ ] **Step 4: Run capture and selection tests**

Run:

```bash
cd extension
node --test youtube/page-capture.test.js youtube/format-selection.test.js
```

Expected: all page-capture and selection tests pass.

- [ ] **Step 5: Commit Task 4**

```bash
git add extension/youtube/page-capture.js extension/youtube/page-capture.test.js
git commit -m "feat: capture logged-in YouTube media requests"
```

## Task 5: Own the Temporary Tab Lifecycle

**Files:**
- Create: `extension/youtube/resolver.test.js`
- Create: `extension/youtube/resolver.js`
- Modify: `extension/background.js`

- [ ] **Step 1: Write failing resolver lifecycle tests**

Build a fake Chrome API in `extension/youtube/resolver.test.js` with:

- `tabs.create` returning incrementing IDs beginning with `{ id: 91 }`;
- `tabs.get` returning the requested ID with `status: 'complete'`;
- `tabs.remove` recording removed IDs;
- `scripting.executeScript` returning
  `[{ result: { status: 'OK', formats: fixtureFormats, resourceUrls: fixtureUrls } }]`;
- in-memory `storage.session.get/set`;
- a minimal `tabs.onUpdated` listener registry.

Test these contracts:

```js
test('resolves through one inactive canonical tab and always closes it', async () => {
  const response = await resolver.handleMessage({
    action: protocol.RUNTIME_RESOLVE,
    requestId: 'req_1',
    videoId: 'dQw4w9WgXcQ',
  }, trustedSender);
  assert.equal(response.ok, true);
  assert.deepEqual(chrome.tabs.create.calls[0], [{
    url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ',
    active: false,
  }]);
  assert.deepEqual(chrome.tabs.remove.calls, [[91]]);
});

test('deduplicates the same video and cancellation closes the last waiter tab', async () => {
  const first = resolver.handleMessage(resolveMessage('req_1'), trustedSender);
  const second = resolver.handleMessage(resolveMessage('req_2'), trustedSender);
  assert.equal(chrome.tabs.create.calls.length, 1);
  await resolver.handleMessage({
    action: protocol.RUNTIME_CANCEL,
    requestId: 'req_1',
  }, trustedSender);
  assert.equal(chrome.tabs.remove.calls.length, 0);
  await resolver.handleMessage({
    action: protocol.RUNTIME_CANCEL,
    requestId: 'req_2',
  }, trustedSender);
  assert.deepEqual(chrome.tabs.remove.calls, [[91]]);
  await Promise.allSettled([first, second]);
});

test('rejects an untrusted sender before opening a tab', async () => {
  const response = await resolver.handleMessage(resolveMessage('req_1'), {
    tab: { id: 2, url: 'https://evil.example/' },
  });
  assert.deepEqual(response, { ok: false, code: 'INTERNAL_ERROR' });
  assert.equal(chrome.tabs.create.calls.length, 0);
});

test('cleans orphan tab IDs stored by a previous worker', async () => {
  chrome.storage.session.data.rssPalYouTubeTabs = [44, 45];
  await resolver.cleanupOrphans();
  assert.deepEqual(chrome.tabs.remove.calls, [[44], [45]]);
  assert.deepEqual(chrome.storage.session.data.rssPalYouTubeTabs, []);
});

test('closes a temporary tab when its RSS Pal requester tab disappears', async () => {
  const pending = resolver.handleMessage(resolveMessage('req_1'), trustedSender);
  await resolver.cancelRequestsForTab(trustedSender.tab.id);
  assert.deepEqual(chrome.tabs.remove.calls, [[91]]);
  await Promise.allSettled([pending]);
});

test('opens at most two different video resolutions concurrently', async () => {
  const first = resolver.handleMessage(resolveMessage('req_1', 'dQw4w9WgXcQ'), trustedSender);
  const second = resolver.handleMessage(resolveMessage('req_2', '2RJiaf0SY8s'), trustedSender);
  const third = await resolver.handleMessage(resolveMessage('req_3', 'aqz-KE-bpKQ'), trustedSender);
  assert.deepEqual(third, { ok: false, code: 'INTERNAL_ERROR' });
  assert.equal(chrome.tabs.create.calls.length, 2);
  await resolver.cancelRequestsForTab(trustedSender.tab.id);
  await Promise.allSettled([first, second]);
});
```

- [ ] **Step 2: Run resolver tests and verify RED**

Run:

```bash
cd extension
node --test youtube/resolver.test.js
```

Expected: FAIL with `Cannot find module './resolver.js'`.

- [ ] **Step 3: Implement the resolver with injected dependencies**

Create `extension/youtube/resolver.js` exporting:

```js
function createYouTubeResolver({
  chromeApi,
  protocol,
  selectPlayback,
  capturePageState,
  now = () => Date.now(),
  tabTimeoutMs = 30000,
  maxConcurrent = 2,
}) {
  const activeByVideo = new Map();
  const requestToVideo = new Map();
  const requestToTab = new Map();
  const orphanKey = 'rssPalYouTubeTabs';

  async function persistTabs() {
    const ids = Array.from(activeByVideo.values())
      .filter((entry) => !entry.closed)
      .map((entry) => entry.tabId)
      .filter(Number.isInteger);
    await chromeApi.storage.session.set({ [orphanKey]: ids });
  }

  async function closeTab(entry) {
    if (!entry || !Number.isInteger(entry.tabId) || entry.closed) return;
    entry.closed = true;
    try {
      await chromeApi.tabs.remove(entry.tabId);
    } catch (_) {
      return;
    } finally {
      await persistTabs();
    }
  }

  function waitForComplete(tabId) {
    return new Promise((resolve) => {
      let finished = false;
      const finish = (ok) => {
        if (finished) return;
        finished = true;
        clearTimeout(timer);
        chromeApi.tabs.onUpdated.removeListener(listener);
        resolve(ok);
      };
      const timer = setTimeout(() => finish(false), tabTimeoutMs);
      function listener(updatedId, info) {
        if (updatedId === tabId && info.status === 'complete') finish(true);
      }
      chromeApi.tabs.onUpdated.addListener(listener);
      chromeApi.tabs.get(tabId)
        .then((tab) => {
          if (tab && tab.status === 'complete') finish(true);
        })
        .catch(() => finish(false));
    });
  }

  async function resolveEntry(entry) {
    try {
      const tab = await chromeApi.tabs.create({
        url: `https://www.youtube.com/watch?v=${entry.videoId}`,
        active: false,
      });
      entry.tabId = tab.id;
      await persistTabs();
      if (!await waitForComplete(tab.id)) return { ok: false, code: 'RESOLVE_TIMEOUT' };
      const injected = await chromeApi.scripting.executeScript({
        target: { tabId: tab.id },
        world: 'MAIN',
        func: capturePageState,
        args: [{ timeoutMs: 15000 }],
      });
      const captured = injected && injected[0] && injected[0].result;
      if (!captured) return { ok: false, code: 'INTERNAL_ERROR' };
      if (captured.status === 'CAPTURE_TIMEOUT') return { ok: false, code: 'RESOLVE_TIMEOUT' };
      return selectPlayback(captured, now());
    } catch (_) {
      return { ok: false, code: 'LOCAL_NETWORK_ERROR' };
    } finally {
      await closeTab(entry);
      activeByVideo.delete(entry.videoId);
      for (const requestId of entry.requestIds) {
        requestToVideo.delete(requestId);
        requestToTab.delete(requestId);
      }
    }
  }

  async function handleResolve(message, senderTabId) {
    const valid = protocol.validateRuntimeResolve(message);
    if (!valid) return { ok: false, code: 'INTERNAL_ERROR' };
    let entry = activeByVideo.get(valid.videoId);
    if (!entry) {
      if (activeByVideo.size >= maxConcurrent) return { ok: false, code: 'INTERNAL_ERROR' };
      entry = {
        videoId: valid.videoId,
        requestIds: new Set(),
        tabId: null,
        closed: false,
        promise: null,
      };
      activeByVideo.set(valid.videoId, entry);
      entry.promise = resolveEntry(entry);
    }
    entry.requestIds.add(valid.requestId);
    requestToVideo.set(valid.requestId, valid.videoId);
    requestToTab.set(valid.requestId, senderTabId);
    return entry.promise;
  }

  async function handleCancel(message) {
    const valid = protocol.validateRuntimeCancel(message);
    if (!valid) return { ok: false, code: 'INTERNAL_ERROR' };
    const videoId = requestToVideo.get(valid.requestId);
    requestToVideo.delete(valid.requestId);
    requestToTab.delete(valid.requestId);
    if (!videoId) return { ok: true };
    const entry = activeByVideo.get(videoId);
    if (!entry) return { ok: true };
    entry.requestIds.delete(valid.requestId);
    if (entry.requestIds.size === 0) await closeTab(entry);
    return { ok: true };
  }

  async function handleMessage(message, sender) {
    if (!protocol.isTrustedSender(sender)) return { ok: false, code: 'INTERNAL_ERROR' };
    if (message && message.action === protocol.RUNTIME_RESOLVE) {
      return handleResolve(message, sender.tab.id);
    }
    if (message && message.action === protocol.RUNTIME_CANCEL) return handleCancel(message);
    return null;
  }

  async function cancelRequestsForTab(tabId) {
    const requestIds = Array.from(requestToTab.entries())
      .filter(([, requestTabId]) => requestTabId === tabId)
      .map(([requestId]) => requestId);
    for (const requestId of requestIds) {
      await handleCancel({
        action: protocol.RUNTIME_CANCEL,
        requestId,
      });
    }
  }

  async function cleanupOrphans() {
    const data = await chromeApi.storage.session.get([orphanKey]);
    const ids = Array.isArray(data[orphanKey]) ? data[orphanKey] : [];
    for (const id of ids) {
      if (!Number.isInteger(id)) continue;
      try {
        await chromeApi.tabs.remove(id);
      } catch (_) {
        continue;
      }
    }
    await chromeApi.storage.session.set({ [orphanKey]: [] });
  }

  return { handleMessage, cancelRequestsForTab, cleanupOrphans };
}
```

Wrap it for CommonJS tests and browser-global
`globalThis.__rssPalCreateYouTubeResolver`.

At the top of `extension/background.js`, replace the existing single import:

```js
importScripts(
  'queue.js',
  'youtube/protocol.js',
  'youtube/format-selection.js',
  'youtube/page-capture.js',
  'youtube/resolver.js',
);

const youtubeResolver = globalThis.__rssPalCreateYouTubeResolver({
  chromeApi: chrome,
  protocol: globalThis.__rssPalYouTubeProtocol,
  selectPlayback: globalThis.__rssPalYouTubeFormatSelection.selectPlayback,
  capturePageState: globalThis.__rssPalCaptureYouTubePageState,
});
```

Add this first branch to the existing `chrome.runtime.onMessage` listener:

```js
  if (msg && (
    msg.action === globalThis.__rssPalYouTubeProtocol.RUNTIME_RESOLVE ||
    msg.action === globalThis.__rssPalYouTubeProtocol.RUNTIME_CANCEL
  )) {
    youtubeResolver.handleMessage(msg, sender)
      .then((response) => sendResponse(response))
      .catch(() => sendResponse({ ok: false, code: 'INTERNAL_ERROR' }));
    return true;
  }
```

Replace the existing direct alarm listeners with:

```js
function startExtensionRuntime() {
  scheduleAlarms();
  youtubeResolver.cleanupOrphans().catch(() => {});
}

chrome.runtime.onInstalled.addListener(startExtensionRuntime);
chrome.runtime.onStartup.addListener(startExtensionRuntime);
youtubeResolver.cleanupOrphans().catch(() => {});
```

Register requester-tab cleanup beside the resolver initialization:

```js
chrome.tabs.onRemoved.addListener((tabId) => {
  youtubeResolver.cancelRequestsForTab(tabId).catch(() => {});
});
```

Do not alter the queue, badge, alarm, or `startSync` branches.

- [ ] **Step 4: Run resolver tests plus a background syntax check**

Run:

```bash
cd extension
node --test youtube/resolver.test.js
node --check background.js
```

Expected: resolver tests pass and `node --check` exits 0.

- [ ] **Step 5: Commit Task 5**

```bash
git add extension/youtube/resolver.js extension/youtube/resolver.test.js extension/background.js
git commit -m "feat: resolve YouTube through temporary tabs"
```

## Task 6: Restrict GoogleVideo CORS to RSS Pal

**Files:**
- Create: `extension/youtube/manifest.test.js`
- Create: `extension/rules/youtube-media-cors.json`
- Modify: `extension/manifest.json`
- Modify: `extension/package.json`

- [ ] **Step 1: Write the failing manifest/ruleset tests**

Create `extension/youtube/manifest.test.js`:

```js
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'manifest.json'), 'utf8'));

test('loads the bridge only on the production RSS Pal origin', () => {
  const bridge = manifest.content_scripts.find((entry) =>
    entry.js.includes('youtube/bridge-content.js'));
  assert.deepEqual(bridge.matches, ['https://rss.morefreeze.top/*']);
  assert.deepEqual(bridge.js, [
    'youtube/protocol.js',
    'youtube/bridge-content.js',
  ]);
});

test('requests DNR host access without cookie or debugger permissions', () => {
  assert.equal(manifest.version, '1.8.4');
  assert.equal(manifest.permissions.includes('declarativeNetRequestWithHostAccess'), true);
  assert.equal(manifest.permissions.includes('cookies'), false);
  assert.equal(manifest.permissions.includes('debugger'), false);
});

test('limits the CORS rule to RSS Pal initiated GoogleVideo XHR', () => {
  const rules = JSON.parse(fs.readFileSync(
    path.join(root, 'rules', 'youtube-media-cors.json'),
    'utf8',
  ));
  assert.equal(rules.length, 1);
  const rule = rules[0];
  assert.deepEqual(rule.condition.initiatorDomains, ['rss.morefreeze.top']);
  assert.deepEqual(rule.condition.requestDomains, ['googlevideo.com']);
  assert.deepEqual(rule.condition.resourceTypes, ['xmlhttprequest']);
  assert.match(rule.condition.urlFilter, /videoplayback/);
  const allowOrigin = rule.action.responseHeaders.find((header) =>
    header.header.toLowerCase() === 'access-control-allow-origin');
  assert.deepEqual(allowOrigin, {
    header: 'Access-Control-Allow-Origin',
    operation: 'set',
    value: 'https://rss.morefreeze.top',
  });
});
```

- [ ] **Step 2: Run the manifest test and verify RED**

Run:

```bash
cd extension
node --test youtube/manifest.test.js
```

Expected: FAIL because the bridge content script, permission, and ruleset are
absent.

- [ ] **Step 3: Add the ruleset and manifest wiring**

Create `extension/rules/youtube-media-cors.json`:

```json
[
  {
    "id": 1,
    "priority": 1,
    "action": {
      "type": "modifyHeaders",
      "responseHeaders": [
        {
          "header": "Access-Control-Allow-Origin",
          "operation": "set",
          "value": "https://rss.morefreeze.top"
        },
        {
          "header": "Access-Control-Expose-Headers",
          "operation": "set",
          "value": "Accept-Ranges, Content-Length, Content-Range"
        },
        {
          "header": "Access-Control-Allow-Methods",
          "operation": "set",
          "value": "GET, HEAD"
        }
      ]
    },
    "condition": {
      "urlFilter": "||googlevideo.com/videoplayback",
      "requestDomains": ["googlevideo.com"],
      "initiatorDomains": ["rss.morefreeze.top"],
      "resourceTypes": ["xmlhttprequest"]
    }
  }
]
```

In `extension/manifest.json`:

- set `"version": "1.8.4"`;
- append `"declarativeNetRequestWithHostAccess"` to `permissions`;
- append the exact RSS Pal content-script block tested above;
- add:

```json
"declarative_net_request": {
  "rule_resources": [
    {
      "id": "youtube_media_cors",
      "enabled": true,
      "path": "rules/youtube-media-cors.json"
    }
  ]
}
```

In `extension/package.json`, set version `1.8.4` and scripts:

```json
{
  "test": "node --test youtube/*.test.js",
  "smoke": "node adapters/twitter/smoke-test.js",
  "check": "npm test && npm run smoke"
}
```

Keep `host_permissions: ["<all_urls>"]` because the existing capture/sync
features already depend on it; do not broaden any other permission.

- [ ] **Step 4: Run the complete extension check**

Run:

```bash
cd extension
npm install
npm run check
node --check background.js
node --check youtube/bridge-content.js
```

Expected: all YouTube unit tests and existing Twitter smoke cases pass, with
no syntax errors.

- [ ] **Step 5: Commit Task 6**

```bash
git add extension/manifest.json extension/package.json \
  extension/rules/youtube-media-cors.json extension/youtube/manifest.test.js
git commit -m "feat: scope GoogleVideo CORS to RSS Pal"
```

## Task 7: Add the Typed Frontend Bridge Client

**Files:**
- Create: `frontend/test/youtubeBridge.test.ts`
- Create: `frontend/src/youtube/bridge.ts`

- [ ] **Step 1: Write failing bridge-client tests**

Create `frontend/test/youtubeBridge.test.ts`. Use a helper that listens for a
page request and posts the corresponding extension response. Cover:

```ts
it('detects extension 1.8.4 through a correlated ping', async () => {
  const onPing = (event: MessageEvent) => {
    if (event.data?.type !== 'RSS_PAL_YOUTUBE_BRIDGE_PING') return
    window.postMessage({
      type: 'RSS_PAL_YOUTUBE_BRIDGE_READY',
      version: '1.8.4',
    }, window.location.origin)
  }
  window.addEventListener('message', onPing)
  await expect(detectYouTubeBridge(100)).resolves.toEqual({
    available: true,
    version: '1.8.4',
    compatible: true,
  })
  window.removeEventListener('message', onPing)
})

it('marks an older extension as incompatible', async () => {
  answerPingWith('1.8.3')
  await expect(detectYouTubeBridge(100)).resolves.toEqual({
    available: true,
    version: '1.8.3',
    compatible: false,
  })
})

it('resolves only the matching request and validates GoogleVideo URLs', async () => {
  answerResolveWith(playbackFixture)
  await expect(resolveYouTubePlayback('dQw4w9WgXcQ', undefined, 100))
    .resolves.toEqual(playbackFixture)
})

it('posts cancellation when aborted', async () => {
  const controller = new AbortController()
  const posted: unknown[] = []
  const listener = (event: MessageEvent) => posted.push(event.data)
  window.addEventListener('message', listener)
  const pending = resolveYouTubePlayback('dQw4w9WgXcQ', controller.signal, 1000)
  controller.abort()
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  expect(posted).toContainEqual(expect.objectContaining({
    type: 'RSS_PAL_YOUTUBE_RESOLVE_CANCEL',
  }))
  window.removeEventListener('message', listener)
})
```

The `playbackFixture` must contain one HTTPS `*.googlevideo.com/videoplayback`
video URL, one audio URL, numeric ranges, 1080 quality, and a future expiry.
Add cases proving a response from another origin and a non-GoogleVideo URL are
ignored/rejected.

- [ ] **Step 2: Run the bridge-client test and verify RED**

Run:

```bash
cd frontend
npx vitest run test/youtubeBridge.test.ts
```

Expected: FAIL because `src/youtube/bridge.ts` does not exist.

- [ ] **Step 3: Implement typed detection and resolution**

Create `frontend/src/youtube/bridge.ts` with exported types from the design and:

```ts
export type ByteRange = {
  start: number
  end: number
}

export type AdaptiveTrack = {
  url: string
  mimeType: string
  codecs: string
  bitrate: number
  initRange: ByteRange
  indexRange: ByteRange
  durationMs: number
  width?: number
  height?: number
  frameRate?: number
  audioSampleRate?: number
}

export type ProgressiveTrack = {
  url: string
  mimeType: string
  height: number
}

export type BrowserPlayback = {
  mode: 'dash' | 'progressive'
  quality: number
  expiresAt: string
  video?: AdaptiveTrack
  audio?: AdaptiveTrack
  progressive?: ProgressiveTrack
}

export const MIN_YOUTUBE_BRIDGE_VERSION = '1.8.4'

export type YouTubeBridgeErrorCode =
  | 'EXTENSION_UNAVAILABLE'
  | 'LOGIN_REQUIRED'
  | 'VIDEO_UNAVAILABLE'
  | 'NO_SUPPORTED_FORMAT'
  | 'RESOLVE_TIMEOUT'
  | 'LOCAL_NETWORK_ERROR'
  | 'PLAYBACK_EXPIRED'
  | 'INTERNAL_ERROR'

export class YouTubeBridgeError extends Error {
  constructor(public readonly code: YouTubeBridgeErrorCode) {
    super(code)
    this.name = 'YouTubeBridgeError'
  }
}

function compareVersions(left: string, right: string): number {
  const a = left.split('.').map(Number)
  const b = right.split('.').map(Number)
  for (let i = 0; i < Math.max(a.length, b.length); i += 1) {
    const diff = (a[i] || 0) - (b[i] || 0)
    if (diff !== 0) return diff
  }
  return 0
}

function validMediaURL(rawURL: string): boolean {
  try {
    const url = new URL(rawURL)
    return url.protocol === 'https:' &&
      (url.hostname === 'googlevideo.com' || url.hostname.endsWith('.googlevideo.com')) &&
      url.pathname.includes('/videoplayback')
  } catch {
    return false
  }
}

export async function detectYouTubeBridge(timeoutMs = 600): Promise<{
  available: boolean
  version?: string
  compatible: boolean
}> {
  return new Promise(resolve => {
    const timer = window.setTimeout(() => {
      window.removeEventListener('message', onMessage)
      resolve({ available: false, compatible: false })
    }, timeoutMs)
    function onMessage(event: MessageEvent) {
      if (event.source !== window || event.origin !== window.location.origin) return
      if (event.data?.type !== 'RSS_PAL_YOUTUBE_BRIDGE_READY') return
      window.clearTimeout(timer)
      window.removeEventListener('message', onMessage)
      const version = String(event.data.version || '')
      resolve({
        available: true,
        version,
        compatible: compareVersions(version, MIN_YOUTUBE_BRIDGE_VERSION) >= 0,
      })
    }
    window.addEventListener('message', onMessage)
    window.postMessage(
      { type: 'RSS_PAL_YOUTUBE_BRIDGE_PING' },
      window.location.origin,
    )
  })
}
```

Implement `resolveYouTubePlayback(videoId, signal, timeoutMs = 50000)` as a
correlated `window.message` promise:

- reject invalid video IDs before posting;
- generate `requestId` with `crypto.randomUUID().replaceAll('-', '_')`;
- accept only same-window, same-origin `RSS_PAL_YOUTUBE_RESOLVE_RESPONSE`;
- require the matching request ID;
- validate every returned URL and numeric range;
- reject `ok: false` with `YouTubeBridgeError(code)`;
- reject timeout with `YouTubeBridgeError('EXTENSION_UNAVAILABLE')`;
- on abort, post `RSS_PAL_YOUTUBE_RESOLVE_CANCEL`, remove listeners, clear the
  timer, and reject with a DOM `AbortError`.

Do not log the response or place it in storage.

- [ ] **Step 4: Run the bridge-client tests and verify GREEN**

Run:

```bash
cd frontend
npx vitest run test/youtubeBridge.test.ts
```

Expected: all bridge-client tests pass.

- [ ] **Step 5: Commit Task 7**

```bash
git add frontend/src/youtube/bridge.ts frontend/test/youtubeBridge.test.ts
git commit -m "feat: add YouTube browser bridge client"
```

## Task 8: Generate an In-Memory DASH Manifest

**Files:**
- Create: `frontend/test/youtubeMpd.test.ts`
- Create: `frontend/src/youtube/mpd.ts`

- [ ] **Step 1: Write failing MPD tests**

Create `frontend/test/youtubeMpd.test.ts` using an adaptive fixture whose signed
URLs include `&` query parameters. Assert:

```ts
const xml = buildYouTubeMpd(playback)
const document = new DOMParser().parseFromString(xml, 'application/xml')

expect(document.querySelector('parsererror')).toBeNull()
expect(document.documentElement.getAttribute('type')).toBe('static')
expect(document.documentElement.getAttribute('mediaPresentationDuration')).toBe('PT120S')
expect(document.querySelector('AdaptationSet[contentType="video"] BaseURL')?.textContent)
  .toBe(playback.video?.url)
expect(document.querySelector('AdaptationSet[contentType="audio"] BaseURL')?.textContent)
  .toBe(playback.audio?.url)
expect(document.querySelector('AdaptationSet[contentType="video"] SegmentBase')
  ?.getAttribute('indexRange')).toBe('740-1251')
expect(document.querySelector('AdaptationSet[contentType="audio"] Initialization')
  ?.getAttribute('range')).toBe('0-721')
```

Add a test proving an unexpected `"` or `<` in codec/URL metadata is XML
escaped, not emitted as markup. Add a test that progressive playback is
rejected because it does not need an MPD.

- [ ] **Step 2: Run MPD tests and verify RED**

Run:

```bash
cd frontend
npx vitest run test/youtubeMpd.test.ts
```

Expected: FAIL because `src/youtube/mpd.ts` does not exist.

- [ ] **Step 3: Implement the MPD builder**

Create `frontend/src/youtube/mpd.ts`:

```ts
import type { AdaptiveTrack, BrowserPlayback } from './bridge'

function xml(value: string | number): string {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;')
}

function range(value: { start: number; end: number }): string {
  return `${value.start}-${value.end}`
}

function representation(track: AdaptiveTrack, kind: 'video' | 'audio'): string {
  const dimensions = kind === 'video'
    ? ` width="${xml(track.width || 0)}" height="${xml(track.height || 0)}"` +
      (track.frameRate ? ` frameRate="${xml(track.frameRate)}"` : '')
    : track.audioSampleRate
      ? ` audioSamplingRate="${xml(track.audioSampleRate)}"`
      : ''
  return [
    `<AdaptationSet contentType="${kind}" mimeType="${xml(track.mimeType)}" segmentAlignment="true">`,
    `<Representation id="${kind}" bandwidth="${xml(track.bitrate)}" codecs="${xml(track.codecs)}"${dimensions}>`,
    `<BaseURL>${xml(track.url)}</BaseURL>`,
    `<SegmentBase indexRange="${range(track.indexRange)}">`,
    `<Initialization range="${range(track.initRange)}"/>`,
    '</SegmentBase>',
    '</Representation>',
    '</AdaptationSet>',
  ].join('')
}

export function buildYouTubeMpd(playback: BrowserPlayback): string {
  if (playback.mode !== 'dash' || !playback.video || !playback.audio) {
    throw new Error('adaptive video and audio tracks are required')
  }
  const durationMs = Math.max(playback.video.durationMs, playback.audio.durationMs)
  if (!Number.isFinite(durationMs) || durationMs <= 0) {
    throw new Error('positive duration is required')
  }
  return [
    '<?xml version="1.0" encoding="UTF-8"?>',
    `<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" profiles="urn:mpeg:dash:profile:isoff-on-demand:2011" minBufferTime="PT1.5S" mediaPresentationDuration="PT${durationMs / 1000}S">`,
    '<Period start="PT0S">',
    representation(playback.video, 'video'),
    representation(playback.audio, 'audio'),
    '</Period>',
    '</MPD>',
  ].join('')
}
```

- [ ] **Step 4: Run MPD and bridge tests**

Run:

```bash
cd frontend
npx vitest run test/youtubeBridge.test.ts test/youtubeMpd.test.ts
```

Expected: both test files pass.

- [ ] **Step 5: Commit Task 8**

```bash
git add frontend/src/youtube/mpd.ts frontend/test/youtubeMpd.test.ts
git commit -m "feat: build local YouTube DASH manifests"
```

## Task 9: Build the Explicit-Click Browser Player

**Files:**
- Create: `frontend/test/YouTubeBrowserPlayer.test.tsx`
- Create: `frontend/src/components/YouTubeBrowserPlayer.tsx`

- [ ] **Step 1: Write failing player-state tests**

Mock `../src/youtube/bridge`, `../src/youtube/mpd`, and `dashjs` using the
existing `YouTubeRelayPlayer.test.tsx` pattern. Cover:

1. detection runs on mount but `resolveYouTubePlayback` is not called until the
   `使用已登录的 Chrome 播放` button is clicked;
2. a compatible bridge plus DASH response initializes dash.js with a Blob URL,
   reports `1080p · 本机 Chrome`, and never autoplays;
3. missing MediaSource uses the progressive URL and truthfully reports 720p;
4. dash.js error switches to returned progressive playback;
5. `LOGIN_REQUIRED` renders `请先在 Chrome 中登录 YouTube` with retry/open
   actions;
6. old extension version renders `请重新加载 RSS Pal 扩展`;
7. unmount aborts the pending resolve request and destroys dash.js;
8. native media failure without progressive fallback triggers one automatic
   re-resolution, then a visible error.

The first test's decisive assertions are:

```tsx
render(
  <YouTubeBrowserPlayer
    videoId="dQw4w9WgXcQ"
    start={30}
    originalURL="https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=30s"
  />,
)

expect(await screen.findByRole('button', {
  name: '使用已登录的 Chrome 播放',
})).toBeTruthy()
expect(mocks.resolve).not.toHaveBeenCalled()
fireEvent.click(screen.getByRole('button', {
  name: '使用已登录的 Chrome 播放',
}))
await waitFor(() => expect(mocks.resolve).toHaveBeenCalledWith(
  'dQw4w9WgXcQ',
  expect.any(AbortSignal),
))
```

- [ ] **Step 2: Run the player tests and verify RED**

Run:

```bash
cd frontend
npx vitest run test/YouTubeBrowserPlayer.test.tsx
```

Expected: FAIL because `YouTubeBrowserPlayer.tsx` does not exist.

- [ ] **Step 3: Implement the player state machine**

Create `frontend/src/components/YouTubeBrowserPlayer.tsx` with:

```ts
interface Props {
  videoId: string
  start?: number
  originalURL: string
}

type Phase =
  | 'checking'
  | 'idle'
  | 'resolving'
  | 'ready'
  | 'unavailable'
  | 'outdated'
  | 'error'
```

The component owns these refs:

```ts
const videoRef = useRef<HTMLVideoElement>(null)
const abortRef = useRef<AbortController | null>(null)
const dashRef = useRef<MediaPlayerClass | null>(null)
const dashErrorRef = useRef<(() => void) | null>(null)
const mpdURLRef = useRef('')
const autoRetryUsedRef = useRef(false)
const mountedRef = useRef(true)
```

On mount, call `detectYouTubeBridge()`. Set `idle` only for a compatible
bridge, `outdated` for a detected version below `1.8.4`, and `unavailable`
after a missing handshake. Detection performs no media request.

Implement `clearPlayback()` to:

- unregister the dash.js error handler;
- destroy the dash.js instance;
- revoke the Blob MPD URL;
- clear `video.src` and call `video.load()`;
- abort any pending bridge request.

Implement `attach(playback)`:

```ts
if (
  playback.mode === 'dash' &&
  playback.video &&
  playback.audio &&
  typeof window.MediaSource !== 'undefined'
) {
  const manifest = buildYouTubeMpd(playback)
  const manifestURL = URL.createObjectURL(
    new Blob([manifest], { type: 'application/dash+xml' }),
  )
  mpdURLRef.current = manifestURL
  const { MediaPlayer } = await import('dashjs')
  const player = MediaPlayer().create()
  dashRef.current = player
  const onDashError = () => {
    if (playback.progressive) {
      player.destroy()
      dashRef.current = null
      setProgressiveURL(playback.progressive.url)
      setQuality(playback.progressive.height)
      setCompatibilityMode(true)
      return
    }
    void retryMediaOnce()
  }
  dashErrorRef.current = onDashError
  player.on(MediaPlayer.events.ERROR, onDashError)
  player.initialize(videoRef.current, manifestURL, false)
} else if (playback.progressive) {
  setProgressiveURL(playback.progressive.url)
  setQuality(playback.progressive.height)
  setCompatibilityMode(true)
} else {
  throw new YouTubeBridgeError('NO_SUPPORTED_FORMAT')
}
setQuality(current => current || playback.quality)
setPhase('ready')
```

`resolveAndAttach()` must:

- create and store a new `AbortController`;
- set `resolving`;
- call `resolveYouTubePlayback(videoId, signal)`;
- reject playback expiring in less than 30 seconds with
  `PLAYBACK_EXPIRED`;
- clear the previous player before attaching the new result;
- map typed errors to Chinese copy without showing exception text or URLs.

`retryMediaOnce()` performs one automatic `resolveAndAttach()` after a dash or
native media error. The next media failure sets `error`. The visible retry
button resets the automatic-retry guard.

Render one `<video controls playsInline preload="metadata">` only after
resolution begins. Its `loadedmetadata` handler applies `start` once when it is
positive and within duration. Do not set `autoPlay`; after resolution the user
uses the native play control.

Use these exact user-facing states:

- idle button: `使用已登录的 Chrome 播放`;
- resolving: `正在通过已登录的 YouTube 准备视频…`;
- unavailable: `需要安装并启用 RSS Pal Chrome 扩展`;
- outdated: `请重新加载 RSS Pal 扩展`;
- login: `请先在 Chrome 中登录 YouTube`;
- local network: `本机无法连接 YouTube，请检查 Clash`;
- other error: `视频暂时无法加载`;
- ready label: `${quality}p · 本机 Chrome`;
- progressive label: `${quality}p · 本机 Chrome · 兼容模式`;
- external link: `在 YouTube 打开`.

- [ ] **Step 4: Run the player tests and verify GREEN**

Run:

```bash
cd frontend
npx vitest run test/YouTubeBrowserPlayer.test.tsx
```

Expected: all eight player-state cases pass.

- [ ] **Step 5: Commit Task 9**

```bash
git add frontend/src/components/YouTubeBrowserPlayer.tsx \
  frontend/test/YouTubeBrowserPlayer.test.tsx
git commit -m "feat: play YouTube through logged-in Chrome"
```

## Task 10: Route Primary and Inline YouTube Media

**Files:**
- Modify: `frontend/src/components/ArticlePlayerCard.tsx`
- Modify: `frontend/src/components/VideoEmbed.tsx`
- Modify: `frontend/test/ArticlePlayerCardYouTube.test.tsx`
- Modify: `frontend/test/VideoEmbed.test.tsx`

- [ ] **Step 1: Change routing tests to require the browser player**

In `ArticlePlayerCardYouTube.test.tsx`, replace the relay mock with:

```tsx
vi.mock('../src/components/YouTubeBrowserPlayer', () => ({
  default: ({
    videoId,
    start,
    originalURL,
  }: {
    videoId: string
    start?: number
    originalURL: string
  }) => <div>browser:{videoId}:{start || 0}:{originalURL}</div>,
}))
```

Assert a stored YouTube item renders:

```ts
expect(screen.getByText(
  'browser:dQw4w9WgXcQ:0:https://www.youtube.com/watch?v=dQw4w9WgXcQ',
)).toBeTruthy()
```

In `VideoEmbed.test.tsx`, mock the same component and assert an inline
placeholder with `start={45}` renders:

```ts
expect(screen.getByText(
  'browser:dQw4w9WgXcQ:45:https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=45s',
)).toBeTruthy()
expect(screen.queryByTitle('youtube video dQw4w9WgXcQ')).toBeNull()
```

Keep the existing Bilibili iframe assertions.

- [ ] **Step 2: Run routing tests and verify RED**

Run:

```bash
cd frontend
npx vitest run test/ArticlePlayerCardYouTube.test.tsx test/VideoEmbed.test.tsx
```

Expected: FAIL because both production components still use the relay/link
placeholder.

- [ ] **Step 3: Route both call sites**

In `ArticlePlayerCard.tsx`, replace the relay import with
`YouTubeBrowserPlayer` and replace the YouTube branch with:

```tsx
if (v.platform === 'youtube') {
  const start = v.start && v.start > 0 ? `&t=${v.start}s` : ''
  return (
    <YouTubeBrowserPlayer
      videoId={v.id}
      start={v.start}
      originalURL={`https://www.youtube.com/watch?v=${v.id}${start}`}
    />
  )
}
```

In `VideoEmbed.tsx`, import `YouTubeBrowserPlayer` and replace the current
YouTube link card with:

```tsx
if (props.platform === 'youtube') {
  const start = props.start && props.start > 0 ? `&t=${props.start}s` : ''
  return (
    <YouTubeBrowserPlayer
      videoId={props.id}
      start={props.start}
      originalURL={`https://www.youtube.com/watch?v=${props.id}${start}`}
    />
  )
}
```

Do not modify the Bilibili `buildSrc`, eager iframe, or
`YouTubeRelayPlayer.tsx`. The old relay stays compiled only if directly
imported elsewhere; the new routing must not import it.

- [ ] **Step 4: Run focused and complete frontend tests**

Run:

```bash
cd frontend
npx vitest run test/ArticlePlayerCardYouTube.test.tsx \
  test/VideoEmbed.test.tsx test/YouTubeBrowserPlayer.test.tsx
npm run check
```

Expected: focused cases and all frontend Vitest/legacy tests pass.

- [ ] **Step 5: Commit Task 10**

```bash
git add frontend/src/components/ArticlePlayerCard.tsx \
  frontend/src/components/VideoEmbed.tsx \
  frontend/test/ArticlePlayerCardYouTube.test.tsx \
  frontend/test/VideoEmbed.test.tsx
git commit -m "feat: route YouTube embeds through Chrome"
```

## Task 11: Full Local Verification and Review

**Files:**
- Verify all changed tracked files.

- [ ] **Step 1: Run the complete extension suite**

```bash
cd extension
npm run check
node --check background.js
for file in youtube/*.js; do node --check "$file"; done
```

Expected: all extension unit/smoke tests and syntax checks pass.

- [ ] **Step 2: Run the complete frontend suite and build**

```bash
cd frontend
npm run check
npm run build
```

Expected: all frontend tests pass and the production bundle builds.

- [ ] **Step 3: Prove server code and Bilibili behavior are untouched**

From the worktree root:

```bash
git diff --name-only bcf98cc..HEAD
git diff --check bcf98cc..HEAD
rg -n "YouTubeRelayPlayer|startYouTubePlayback" \
  frontend/src/components/ArticlePlayerCard.tsx \
  frontend/src/components/VideoEmbed.tsx
git diff bcf98cc..HEAD -- backend docker-compose.yml frontend/nginx.conf
```

Expected:

- changed files are only the design/plan, extension, and frontend files listed
  in this plan;
- `git diff --check` succeeds;
- neither routing component references relay symbols;
- the backend, Compose, and Nginx diff is empty.

- [ ] **Step 4: Perform a security-focused self-review**

Inspect the actual diff and confirm:

```text
[ ] production origin is checked in both content script and background
[ ] no cookie, debugger, webRequest, tabCapture, or nativeMessaging permission
[ ] no arbitrary URL, code, headers, or selector accepted from the page
[ ] temporary tab closes on success, failure, timeout, cancellation, and orphan cleanup
[ ] signed URLs are not logged or persisted
[ ] DNR rule requires rss.morefreeze.top initiator and googlevideo.com target
[ ] bridge response validates HTTPS GoogleVideo URLs and numeric ranges
[ ] frontend does not call /youtube-playback or /api/media/youtube/
[ ] Bilibili iframe branch is unchanged
```

Fix any failing item with a focused test before continuing.

- [ ] **Step 5: Commit verification-only fixes if the review found any**

If the review changed tracked code, run the affected suite and commit only
those exact files:

```bash
git add extension frontend
git commit -m "fix: harden YouTube browser bridge"
```

If no tracked file changed, do not create an empty commit.

## Task 12: Merge, Reload, Deploy Frontend Only, and Verify Live

**Files/targets:**
- Local `/Users/bytedance/mygit/rss-pal` main worktree.
- Chrome unpacked extension at
  `/Users/bytedance/mygit/rss-pal/extension`.
- GitHub `origin/master`.
- Beijing `/opt/rss-pal`, reached only through `oci-rss-pal`.
- Public `https://rss.morefreeze.top`.

- [ ] **Step 1: Fast-forward local master and push**

From the main worktree:

```bash
cd /Users/bytedance/mygit/rss-pal
git status --short --branch
git merge --ff-only feat/youtube-browser-bridge
git push origin master
git rev-parse --short HEAD
git rev-parse --short origin/master
```

Expected: local and remote master point to the same verified commit. Existing
untracked SQL backups and `rss-pal-course/` remain untouched.

- [ ] **Step 2: Reload the unpacked RSS Pal extension**

Use the user's existing Chrome profile:

1. open `chrome://extensions`;
2. enable Developer mode if it is already disabled;
3. locate RSS Pal and click Reload;
4. if RSS Pal is not installed, choose Load unpacked and select
   `/Users/bytedance/mygit/rss-pal/extension`;
5. open its details and confirm version `1.8.4`;
6. confirm Chrome does not request `cookies` or `debugger`.

Expected: the extension loads without manifest/service-worker errors and its
service worker remains inspectable.

- [ ] **Step 3: Preserve the running Beijing frontend image**

Never SSH directly from the company/Japan egress. Use:

```bash
ssh -o ControlMaster=no -o ControlPath=none \
  -o ProxyJump=oci-rss-pal tencent-rss-pal
```

On Beijing:

```bash
cd /opt/rss-pal
FRONTEND_CONTAINER=$(docker compose ps -q frontend)
FRONTEND_IMAGE_ID=$(docker inspect --format='{{.Image}}' "$FRONTEND_CONTAINER")
FRONTEND_IMAGE_NAME=$(docker inspect --format='{{.Config.Image}}' "$FRONTEND_CONTAINER")
FRONTEND_IMAGE_REPO=${FRONTEND_IMAGE_NAME%:*}
ROLLBACK_FRONTEND_IMAGE="${FRONTEND_IMAGE_REPO}:rollback-youtube-browser-bridge"
docker image tag "$FRONTEND_IMAGE_ID" "$ROLLBACK_FRONTEND_IMAGE"
docker image inspect "$ROLLBACK_FRONTEND_IMAGE" --format '{{.Id}}'
```

Expected: the rollback tag resolves to the previously running frontend image.

- [ ] **Step 4: Update source and rebuild only the frontend**

On Beijing:

```bash
cd /opt/rss-pal
git fetch --no-tags https://github.com/morefreeze/rss-pal.git master
git merge --ff-only FETCH_HEAD
docker compose build frontend
docker compose up -d --no-deps frontend
docker compose ps frontend api worker postgres
```

Expected: frontend is recreated and running; API, worker, and database remain
running on their existing images. Do not run `scripts/auto_deploy.sh`, do not
build `api`, and do not start/recreate `youtube-pot`.

- [ ] **Step 5: Verify public health before media testing**

From the local machine:

```bash
curl --noproxy '*' -fsS https://rss.morefreeze.top/api/health
curl --noproxy '*' -fsS -o /dev/null -w '%{http_code}\n' \
  https://rss.morefreeze.top/articles/2401
curl --noproxy '*' -fsS -o /dev/null -w '%{http_code}\n' \
  https://rss.morefreeze.top/articles/2391
```

Expected: health reports `ok`; both article routes return HTTP 200.

- [ ] **Step 6: Verify logged-in Chrome playback**

In the user's existing signed-in Chrome:

1. open `https://rss.morefreeze.top/articles/2401`;
2. confirm the card says `使用已登录的 Chrome 播放`;
3. confirm no YouTube/GoogleVideo request happens before clicking;
4. click the button and observe one inactive YouTube tab open and close;
5. press the native play control;
6. confirm the label reports 1080p when available, otherwise 720p;
7. confirm audio/video stay synchronized;
8. seek at least ten minutes forward and confirm playback resumes;
9. inspect network activity and confirm media Range requests go directly to
   `*.googlevideo.com/videoplayback`;
10. confirm no request is made to `/api/articles/2401/youtube-playback` or
    `/api/media/youtube/`;
11. disable the extension temporarily and confirm RSS Pal shows the explicit
    extension-unavailable state plus `在 YouTube 打开`;
12. re-enable the extension.

Expected: playback succeeds through local Chrome/Clash with no iframe or server
media relay.

- [ ] **Step 7: Verify Bilibili regression and server non-use**

Open `https://rss.morefreeze.top/articles/2391`, play its Bilibili video, and
confirm the existing `player.bilibili.com` iframe still loads.

On Beijing, inspect only fresh frontend access logs:

```bash
cd /opt/rss-pal
if docker compose logs --since 15m frontend \
  | grep -E '/api/articles/[0-9]+/youtube-playback|/api/media/youtube/'; then
  echo 'unexpected-server-youtube-media-request'
  exit 1
else
  echo 'no-server-youtube-media-request'
fi
```

Expected: `no-server-youtube-media-request`.

- [ ] **Step 8: Roll back the frontend runtime on acceptance failure**

If health, extension detection, direct DASH requests, audio/video sync,
seeking, or Bilibili regression fails, run on Beijing:

```bash
cd /opt/rss-pal
FRONTEND_CONTAINER=$(docker compose ps -q frontend)
FRONTEND_IMAGE_NAME=$(docker inspect --format='{{.Config.Image}}' "$FRONTEND_CONTAINER")
FRONTEND_IMAGE_REPO=${FRONTEND_IMAGE_NAME%:*}
ROLLBACK_FRONTEND_IMAGE="${FRONTEND_IMAGE_REPO}:rollback-youtube-browser-bridge"
docker image tag "$ROLLBACK_FRONTEND_IMAGE" "$FRONTEND_IMAGE_NAME"
docker compose up -d --no-deps --force-recreate frontend
docker compose ps frontend api
```

Then rerun the public health checks. Leave DNS, OCI, Nginx, database, API,
worker, and source history unchanged. The new extension is inert when the old
frontend does not invoke it, so it does not need a destructive local rollback.
