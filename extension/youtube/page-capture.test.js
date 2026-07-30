'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const captureYouTubePageState = require('./page-capture');

function createClock(start = 0) {
  let current = start;
  const sleeps = [];

  return {
    now() {
      return current;
    },
    async sleep(delayMs) {
      sleeps.push(delayMs);
      current += delayMs;
    },
    sleeps,
  };
}

function createEnvironment(player, overrides = {}) {
  const clock = overrides.clock || createClock();

  return {
    document: {
      getElementById(id) {
        assert.equal(id, 'movie_player');
        return player;
      },
    },
    performance: {
      getEntriesByType(type) {
        assert.equal(type, 'resource');
        return [];
      },
    },
    root: {},
    now: clock.now,
    sleep: clock.sleep,
    ...overrides,
    clock,
  };
}

function videoFormat(overrides = {}) {
  return {
    itag: 137,
    mimeType: 'video/mp4; codecs="avc1.640028"',
    bitrate: 4_500_000,
    width: 1920,
    height: 1080,
    fps: 30,
    approxDurationMs: '600000',
    initRange: { start: '0', end: '739' },
    indexRange: { start: '740', end: '1200' },
    ...overrides,
  };
}

function audioFormat(overrides = {}) {
  return {
    itag: 140,
    mimeType: 'audio/mp4; codecs="mp4a.40.2"',
    bitrate: 129_000,
    approxDurationMs: '600000',
    initRange: { start: '0', end: '721' },
    indexRange: { start: '722', end: '1050' },
    audioQuality: 'AUDIO_QUALITY_MEDIUM',
    audioSampleRate: '48000',
    audioChannels: 2,
    ...overrides,
  };
}

function progressiveFormat(overrides = {}) {
  return {
    itag: 22,
    mimeType: 'video/mp4; codecs="avc1.64001F, mp4a.40.2"',
    bitrate: 2_500_000,
    width: 1280,
    height: 720,
    fps: 30,
    approxDurationMs: '600000',
    audioQuality: 'AUDIO_QUALITY_MEDIUM',
    ...overrides,
  };
}

function okResponse(adaptiveFormats = [], formats = []) {
  return {
    playabilityStatus: {
      status: 'OK',
      reason: 'private playability reason',
    },
    streamingData: {
      adaptiveFormats,
      formats,
    },
    privateAccountData: {
      email: 'private@example.com',
    },
  };
}

function directUrl(itag, suffix = '') {
  return (
    `https://rr1---sn-a5mekn6z.googlevideo.com/videoplayback?itag=${itag}` +
    `&sig=signature-${itag}${suffix}`
  );
}

test('exports one standalone async function through CommonJS and the browser global', async () => {
  assert.equal(typeof captureYouTubePageState, 'function');
  assert.equal(captureYouTubePageState.name, 'captureYouTubePageState');
  assert.equal(captureYouTubePageState.constructor.name, 'AsyncFunction');
  assert.deepEqual(Object.keys(captureYouTubePageState), []);

  const source = fs.readFileSync(require.resolve('./page-capture'), 'utf8');
  const context = vm.createContext({});
  vm.runInContext(source, context);

  assert.equal(
    typeof context.__rssPalCaptureYouTubePageState,
    'function',
  );
  assert.equal(context.captureYouTubePageState, undefined);

  const serialized = vm.runInNewContext(
    `(${captureYouTubePageState.toString()})`,
  );
  const result = await serialized(
    { timeoutMs: 1000 },
    {
      document: {
        getElementById() {
          return {
            getPlayerResponse() {
              return okResponse([], [
                progressiveFormat({ url: directUrl(22) }),
              ]);
            },
            playVideo() {},
            pauseVideo() {},
          };
        },
      },
      performance: {
        getEntriesByType() {
          return [];
        },
      },
      root: {},
      now: () => 0,
      sleep: async () => {},
    },
  );

  assert.equal(result.status, 'OK');
  assert.equal(result.formats.length, 1);
});

test('captures finalized 1080p video and audio requests without leaking private player fields', async () => {
  const events = [];
  const clock = createClock(10_000);
  const video = videoFormat({
    signatureCipher: 's=private-signature&sp=sig',
    cipher: 'private-cipher',
    unknown: { secret: true },
  });
  const audio = audioFormat({
    unknown: 'private',
    headers: { Cookie: 'private-cookie' },
  });
  const response = okResponse([video, audio]);
  const videoUrl = directUrl(137, '&range=0-999');
  const audioUrl = directUrl(140, '&range=0-999');

  const player = {
    getPlayerResponse() {
      return response;
    },
    getAvailableQualityLevels() {
      return ['hd1080', 'hd720', 'large'];
    },
    setPlaybackQualityRange(minimum, maximum) {
      events.push(['quality-range', minimum, maximum]);
    },
    mute() {
      events.push(['mute']);
    },
    playVideo() {
      events.push(['play']);
    },
    pauseVideo() {
      events.push(['pause']);
    },
  };
  const environment = createEnvironment(player, {
    clock,
    performance: {
      getEntriesByType(type) {
        assert.equal(type, 'resource');
        events.push(['resources']);
        return clock.now() >= 10_250
          ? [{ name: videoUrl }, { name: audioUrl }]
          : [];
      },
    },
  });

  const result = await captureYouTubePageState(
    { timeoutMs: 15_000 },
    environment,
  );

  assert.deepEqual(result, {
    status: 'OK',
    formats: [
      {
        itag: 137,
        mimeType: 'video/mp4; codecs="avc1.640028"',
        bitrate: 4_500_000,
        width: 1920,
        height: 1080,
        fps: 30,
        approxDurationMs: '600000',
        initRange: { start: '0', end: '739' },
        indexRange: { start: '740', end: '1200' },
      },
      {
        itag: 140,
        mimeType: 'audio/mp4; codecs="mp4a.40.2"',
        bitrate: 129_000,
        approxDurationMs: '600000',
        initRange: { start: '0', end: '721' },
        indexRange: { start: '722', end: '1050' },
        audioQuality: 'AUDIO_QUALITY_MEDIUM',
        audioSampleRate: '48000',
        audioChannels: 2,
      },
    ],
    resourceUrls: [videoUrl, audioUrl],
  });
  assert.deepEqual(clock.sleeps, [250]);
  assert.deepEqual(events, [
    ['quality-range', 'hd1080', 'hd1080'],
    ['mute'],
    ['play'],
    ['resources'],
    ['resources'],
    ['pause'],
  ]);
  assert.equal(JSON.stringify(result).includes('private'), false);
  assert.equal(JSON.stringify(result).includes('signatureCipher'), false);
  assert.equal(JSON.stringify(result).includes('unknown'), false);
});

test('falls back to hd720 and the single-quality player method', async () => {
  const calls = [];
  const player = {
    getPlayerResponse() {
      return okResponse([], [
        progressiveFormat({ url: directUrl(22) }),
      ]);
    },
    getAvailableQualityLevels() {
      return ['large', 'hd720'];
    },
    setPlaybackQuality(quality) {
      calls.push(['quality', quality]);
    },
    mute() {
      calls.push(['mute']);
    },
    playVideo() {
      calls.push(['play']);
    },
    pauseVideo() {
      calls.push(['pause']);
    },
  };

  const result = await captureYouTubePageState(
    {},
    createEnvironment(player),
  );

  assert.equal(result.status, 'OK');
  assert.deepEqual(calls, [
    ['quality', 'hd720'],
    ['mute'],
    ['play'],
    ['pause'],
  ]);
});

test('returns immediately when finalized direct adaptive URLs already form a pair', async () => {
  const clock = createClock();
  let pauseCalls = 0;
  const player = {
    getPlayerResponse() {
      return okResponse([
        videoFormat({ url: directUrl(137) }),
        audioFormat({ url: directUrl(140) }),
      ]);
    },
    getAvailableQualityLevels() {
      return [];
    },
    playVideo() {},
    pauseVideo() {
      pauseCalls += 1;
    },
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 20_000 },
    createEnvironment(player, { clock }),
  );

  assert.equal(result.status, 'OK');
  assert.deepEqual(result.resourceUrls, []);
  assert.deepEqual(clock.sleeps, []);
  assert.equal(pauseCalls, 1);
});

test('returns the exact non-OK status envelope without touching playback or private reasons', async () => {
  const forbidden = () => {
    throw new Error('playback method must not be touched');
  };
  const player = {
    getPlayerResponse() {
      return {
        playabilityStatus: {
          status: 'LOGIN_REQUIRED',
          reason: 'Sign in as private@example.com',
        },
        streamingData: {
          formats: [progressiveFormat({ url: directUrl(22) })],
        },
      };
    },
    getAvailableQualityLevels: forbidden,
    setPlaybackQuality: forbidden,
    mute: forbidden,
    playVideo: forbidden,
    pauseVideo: forbidden,
  };

  assert.deepEqual(
    await captureYouTubePageState({}, createEnvironment(player)),
    {
      status: 'LOGIN_REQUIRED',
      formats: [],
      resourceUrls: [],
    },
  );
});

test('returns the exact timeout envelope and clamps timeout options', async (t) => {
  for (const scenario of [
    { name: 'minimum', options: { timeoutMs: 10 }, elapsed: 1000 },
    { name: 'maximum', options: { timeoutMs: 99_999 }, elapsed: 20_000 },
    { name: 'default', options: {}, elapsed: 15_000 },
  ]) {
    await t.test(scenario.name, async () => {
      const clock = createClock();
      const environment = {
        document: {
          getElementById() {
            return null;
          },
        },
        performance: {
          getEntriesByType() {
            throw new Error('must not inspect resources without a player');
          },
        },
        root: {},
        now: clock.now,
        sleep: clock.sleep,
      };

      assert.deepEqual(
        await captureYouTubePageState(scenario.options, environment),
        {
          status: 'CAPTURE_TIMEOUT',
          formats: [],
          resourceUrls: [],
        },
      );
      assert.equal(clock.now(), scenario.elapsed);
      assert.equal(clock.sleeps.every((delay) => delay > 0), true);
      assert.equal(clock.sleeps.every((delay) => delay <= 250), true);
    });
  }
});

test('treats malformed or throwing quality-level APIs as empty', async (t) => {
  for (const scenario of [
    {
      name: 'non-array',
      getAvailableQualityLevels() {
        return 'hd1080';
      },
    },
    {
      name: 'throws',
      getAvailableQualityLevels() {
        throw new Error('quality unavailable');
      },
    },
  ]) {
    await t.test(scenario.name, async () => {
      let qualityCalls = 0;
      let pauseCalls = 0;
      const player = {
        getPlayerResponse() {
          return okResponse([], [
            progressiveFormat({ url: directUrl(22) }),
          ]);
        },
        getAvailableQualityLevels: scenario.getAvailableQualityLevels,
        setPlaybackQuality() {
          qualityCalls += 1;
        },
        playVideo() {},
        pauseVideo() {
          pauseCalls += 1;
        },
      };

      const result = await captureYouTubePageState(
        {},
        createEnvironment(player),
      );

      assert.equal(result.status, 'OK');
      assert.equal(qualityCalls, 0);
      assert.equal(pauseCalls, 1);
    });
  }
});

test('pauses and returns a bounded envelope when playback or resource inspection throws', async (t) => {
  for (const scenario of [
    {
      name: 'playVideo throws',
      playVideo() {
        throw new Error('private playback failure');
      },
      performance: {
        getEntriesByType() {
          return [];
        },
      },
    },
    {
      name: 'performance getter throws',
      playVideo() {},
      performance: {
        getEntriesByType() {
          throw new Error('private performance failure');
        },
      },
    },
  ]) {
    await t.test(scenario.name, async () => {
      let pauseCalls = 0;
      const player = {
        getPlayerResponse() {
          return okResponse([], [progressiveFormat()]);
        },
        playVideo: scenario.playVideo,
        pauseVideo() {
          pauseCalls += 1;
        },
      };

      const result = await captureYouTubePageState(
        {},
        createEnvironment(player, {
          performance: scenario.performance,
        }),
      );

      assert.deepEqual(result, {
        status: 'CAPTURE_TIMEOUT',
        formats: [],
        resourceUrls: [],
      });
      assert.equal(pauseCalls, 1);
      assert.equal(JSON.stringify(result).includes('private'), false);
    });
  }
});

test('accepts only HTTPS GoogleVideo videoplayback resources with positive query or path itags', async () => {
  const acceptedQuery =
    'https://rr1---sn-a5mekn6z.googlevideo.com/videoplayback?itag=22&range=0-99';
  const acceptedPath =
    'https://googlevideo.com/videoplayback/chunk/itag/140/expire/1780000600/';
  const rejected = [
    'http://googlevideo.com/videoplayback?itag=22',
    'https://googlevideo.com.evil/videoplayback?itag=22',
    'https://evilgooglevideo.com/videoplayback?itag=22',
    'https://googlevideo.com/videoplaybackevil?itag=22',
    'https://googlevideo.com/videoplayback',
    'https://googlevideo.com/videoplayback?itag=0',
    'https://googlevideo.com/videoplayback?itag=-22',
    'https://googlevideo.com/videoplayback?itag=2.2',
    'not a URL',
  ];
  let pauseCalls = 0;
  const player = {
    getPlayerResponse() {
      return okResponse([], [progressiveFormat()]);
    },
    playVideo() {},
    pauseVideo() {
      pauseCalls += 1;
    },
  };
  const result = await captureYouTubePageState(
    {},
    createEnvironment(player, {
      performance: {
        getEntriesByType() {
          return [
            ...rejected.map((name) => ({ name })),
            { name: acceptedQuery },
            { name: acceptedPath },
            { name: 123 },
            {},
          ];
        },
      },
    }),
  );

  assert.deepEqual(result.resourceUrls, [acceptedQuery, acceptedPath]);
  assert.equal(pauseCalls, 1);
});

test('deduplicates formats and resource URLs and caps each collection at 256', async () => {
  const first = progressiveFormat({ itag: 1, bitrate: 111 });
  const duplicate = progressiveFormat({ itag: 1, bitrate: 999 });
  const remaining = Array.from({ length: 299 }, (_, index) =>
    progressiveFormat({ itag: index + 2 }),
  );
  const resourceUrls = Array.from({ length: 300 }, (_, index) =>
    directUrl(index + 1),
  );
  const player = {
    getPlayerResponse() {
      return okResponse([first], [duplicate, ...remaining]);
    },
    playVideo() {},
    pauseVideo() {},
  };

  const result = await captureYouTubePageState(
    {},
    createEnvironment(player, {
      performance: {
        getEntriesByType() {
          return [
            { name: resourceUrls[0] },
            ...resourceUrls.map((name) => ({ name })),
          ];
        },
      },
    }),
  );

  assert.equal(result.formats.length, 256);
  assert.equal(
    new Set(result.formats.map((format) => format.itag)).size,
    256,
  );
  assert.equal(result.formats[0].bitrate, 111);
  assert.equal(result.resourceUrls.length, 256);
  assert.equal(new Set(result.resourceUrls).size, 256);
  assert.deepEqual(result.resourceUrls, resourceUrls.slice(0, 256));
});

test('does not mutate or retain mutable format objects from the player response', async () => {
  const response = okResponse([
    videoFormat({ url: directUrl(137) }),
    audioFormat({ url: directUrl(140) }),
  ]);
  const snapshot = structuredClone(response);
  const player = {
    getPlayerResponse() {
      return response;
    },
    playVideo() {},
    pauseVideo() {},
  };

  const result = await captureYouTubePageState(
    {},
    createEnvironment(player),
  );

  assert.deepEqual(response, snapshot);
  assert.notEqual(result.formats[0], response.streamingData.adaptiveFormats[0]);
  assert.notEqual(
    result.formats[0].initRange,
    response.streamingData.adaptiveFormats[0].initRange,
  );
  result.formats[0].initRange.start = 'changed';
  assert.equal(
    response.streamingData.adaptiveFormats[0].initRange.start,
    '0',
  );
});

test('falls back to ytInitialPlayerResponse when getPlayerResponse fails', async () => {
  const fallbackResponse = okResponse([], [
    progressiveFormat({ url: directUrl(22) }),
  ]);
  let pauseCalls = 0;
  const player = {
    getPlayerResponse() {
      throw new Error('transient player response failure');
    },
    playVideo() {},
    pauseVideo() {
      pauseCalls += 1;
    },
  };

  const result = await captureYouTubePageState(
    {},
    createEnvironment(player, {
      root: {
        ytInitialPlayerResponse: fallbackResponse,
      },
    }),
  );

  assert.equal(result.status, 'OK');
  assert.equal(result.formats.length, 1);
  assert.equal(pauseCalls, 1);
});
