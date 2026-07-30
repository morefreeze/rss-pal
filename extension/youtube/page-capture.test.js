'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const captureYouTubePageState = require('./page-capture');
const { selectPlayback } = require('./format-selection');
const TARGET_VIDEO_ID = 'dQw4w9WgXcQ';
const OTHER_VIDEO_ID = 'aaaaaaaaaaa';
const NOW_MS = 1_770_000_000_000;

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

function responseForVideo(
  videoId,
  adaptiveFormats = [],
  formats = [],
) {
  return {
    ...okResponse(adaptiveFormats, formats),
    videoDetails: { videoId },
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
      return okResponse([
        videoFormat({
          itag: 136,
          width: 1280,
          height: 720,
          url: directUrl(136),
        }),
        audioFormat({ url: directUrl(140) }),
      ]);
    },
    getAvailableQualityLevels() {
      return ['large'];
    },
    setPlaybackQualityRange(minimum, maximum) {
      calls.push(['quality-range', minimum, maximum]);
      throw new Error('private range API failure');
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
    ['quality-range', 'hd720', 'hd720'],
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

test('polls past an empty transient response until target playback metadata is usable', async () => {
  const clock = createClock();
  let responseReads = 0;
  let pauseCalls = 0;
  const player = {
    getPlayerResponse() {
      responseReads += 1;
      if (responseReads === 1) {
        return {};
      }
      if (responseReads === 2) {
        return okResponse();
      }
      return responseForVideo(TARGET_VIDEO_ID, [], [
        progressiveFormat({ url: directUrl(22) }),
      ]);
    },
    getVideoData() {
      return { video_id: TARGET_VIDEO_ID };
    },
    playVideo() {},
    pauseVideo() {
      pauseCalls += 1;
    },
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 1000, videoId: TARGET_VIDEO_ID },
    createEnvironment(player, { clock }),
  );

  assert.equal(result.status, 'OK');
  assert.equal(responseReads, 4);
  assert.deepEqual(clock.sleeps, [250, 250]);
  assert.equal(pauseCalls, 1);
});

test('waits for both response and live player data to match the target video', async () => {
  const clock = createClock();
  let responseReads = 0;
  let responseVideoId = OTHER_VIDEO_ID;
  let playerVideoId = TARGET_VIDEO_ID;
  const player = {
    getPlayerResponse() {
      responseReads += 1;
      responseVideoId =
        responseReads === 1 ? OTHER_VIDEO_ID : TARGET_VIDEO_ID;
      playerVideoId =
        responseReads === 2 ? OTHER_VIDEO_ID : TARGET_VIDEO_ID;
      return responseForVideo(responseVideoId, [], [
        progressiveFormat({ url: directUrl(22) }),
      ]);
    },
    getVideoData() {
      return { video_id: playerVideoId };
    },
    playVideo() {},
    pauseVideo() {},
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 1000, videoId: TARGET_VIDEO_ID },
    createEnvironment(player, { clock }),
  );

  assert.equal(result.status, 'OK');
  assert.equal(responseReads, 4);
  assert.deepEqual(clock.sleeps, [250, 250]);
});

test('waits out ads, clears resource history, and keeps only the latest current-format URL per itag', async () => {
  const clock = createClock();
  const oldVideoUrl = directUrl(137, '&range=old');
  const oldAudioUrl = directUrl(140, '&range=old');
  const firstVideoUrl = directUrl(137, '&range=0-999');
  const latestVideoUrl = directUrl(137, '&range=1000-1999');
  const audioUrl = directUrl(140, '&range=0-999');
  const unknownUrl = directUrl(999, '&range=0-999');
  const events = [];
  let entries = [{ name: oldVideoUrl }, { name: oldAudioUrl }];
  let classChecks = 0;
  const adStates = [1, 0];
  const player = {
    getPlayerResponse() {
      return responseForVideo(TARGET_VIDEO_ID, [
        videoFormat(),
        audioFormat(),
      ]);
    },
    getVideoData() {
      return { video_id: TARGET_VIDEO_ID };
    },
    classList: {
      contains(name) {
        assert.equal(name, 'ad-showing');
        classChecks += 1;
        return classChecks === 1;
      },
    },
    getAdState() {
      return adStates.shift() ?? 0;
    },
    setPlaybackQualityRange(minimum, maximum) {
      events.push(['quality', minimum, maximum]);
    },
    mute() {
      events.push(['mute']);
    },
    playVideo() {
      events.push(['play']);
      entries.push(
        { name: unknownUrl },
        { name: firstVideoUrl },
        { name: latestVideoUrl },
        { name: audioUrl },
      );
    },
    pauseVideo() {
      events.push(['pause']);
    },
  };
  const performance = {
    setResourceTimingBufferSize(size) {
      events.push(['buffer-size', size]);
    },
    clearResourceTimings() {
      events.push(['clear']);
      entries = [];
    },
    getEntriesByType(type) {
      assert.equal(type, 'resource');
      return entries;
    },
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 1000, videoId: TARGET_VIDEO_ID },
    createEnvironment(player, { clock, performance }),
  );

  assert.deepEqual(clock.sleeps, [250, 250]);
  assert.deepEqual(result.resourceUrls, [latestVideoUrl, audioUrl]);
  assert.equal(result.resourceUrls.includes(oldVideoUrl), false);
  assert.equal(result.resourceUrls.includes(oldAudioUrl), false);
  assert.equal(result.resourceUrls.includes(unknownUrl), false);
  assert.equal(events.findIndex(([name]) => name === 'clear') <
    events.findIndex(([name]) => name === 'play'), true);
  assert.deepEqual(events, [
    ['buffer-size', 1000],
    ['clear'],
    ['quality', 'hd1080', 'hd1080'],
    ['mute'],
    ['play'],
    ['pause'],
  ]);
});

test('rejects same-itag pre-roll requests and accepts only reconfirmed target streams', async () => {
  const clock = createClock();
  const adVideoUrl = directUrl(137, '&source=ad');
  const adAudioUrl = directUrl(140, '&source=ad');
  const targetVideoUrl = directUrl(137, '&source=target');
  const targetAudioUrl = directUrl(140, '&source=target');
  let playbackStarted = false;
  let clearCalls = 0;
  let pauseCalls = 0;
  const player = {
    getPlayerResponse() {
      return responseForVideo(TARGET_VIDEO_ID, [
        videoFormat(),
        audioFormat(),
      ]);
    },
    getVideoData() {
      return { video_id: TARGET_VIDEO_ID };
    },
    getAdState() {
      return playbackStarted && clock.now() < 250 ? 1 : 0;
    },
    setPlaybackQualityRange() {},
    playVideo() {
      playbackStarted = true;
    },
    pauseVideo() {
      pauseCalls += 1;
    },
  };
  const performance = {
    setResourceTimingBufferSize() {},
    clearResourceTimings() {
      clearCalls += 1;
    },
    getEntriesByType() {
      if (clock.now() < 500) {
        return [{ name: adVideoUrl }, { name: adAudioUrl }];
      }
      return [
        { name: targetVideoUrl },
        { name: targetAudioUrl },
      ];
    },
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 1000, videoId: TARGET_VIDEO_ID },
    createEnvironment(player, { clock, performance }),
  );
  const selection = selectPlayback(result, NOW_MS);

  assert.equal(result.status, 'OK');
  assert.deepEqual(result.resourceUrls, [
    targetVideoUrl,
    targetAudioUrl,
  ]);
  assert.equal(result.resourceUrls.includes(adVideoUrl), false);
  assert.equal(result.resourceUrls.includes(adAudioUrl), false);
  assert.equal(clearCalls >= 3, true);
  assert.equal(selection.ok, true);
  assert.equal(selection.playback.mode, 'dash');
  assert.equal(selection.playback.quality, 1080);
  assert.equal(pauseCalls, 1);
});

test('times out while a post-play ad remains active and never returns its streams', async () => {
  const clock = createClock();
  const adVideoUrl = directUrl(137, '&source=persistent-ad');
  const adAudioUrl = directUrl(140, '&source=persistent-ad');
  let playbackStarted = false;
  let pauseCalls = 0;
  const player = {
    getPlayerResponse() {
      return responseForVideo(TARGET_VIDEO_ID, [
        videoFormat(),
        audioFormat(),
      ]);
    },
    getVideoData() {
      return { video_id: TARGET_VIDEO_ID };
    },
    getAdState() {
      return playbackStarted ? 1 : 0;
    },
    setPlaybackQualityRange() {},
    playVideo() {
      playbackStarted = true;
    },
    pauseVideo() {
      pauseCalls += 1;
    },
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 1000, videoId: TARGET_VIDEO_ID },
    createEnvironment(player, {
      clock,
      performance: {
        clearResourceTimings() {},
        getEntriesByType() {
          return [{ name: adVideoUrl }, { name: adAudioUrl }];
        },
      },
    }),
  );

  assert.deepEqual(result, {
    status: 'CAPTURE_TIMEOUT',
    formats: [],
    resourceUrls: [],
  });
  assert.equal(clock.now(), 1000);
  assert.equal(pauseCalls, 1);
});

test('fails closed when an expected video has no positive identity source', async () => {
  const clock = createClock();
  let playCalls = 0;
  const player = {
    getPlayerResponse() {
      return okResponse([
        videoFormat({ url: directUrl(137) }),
        audioFormat({ url: directUrl(140) }),
      ]);
    },
    playVideo() {
      playCalls += 1;
    },
    pauseVideo() {},
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 1000, videoId: TARGET_VIDEO_ID },
    createEnvironment(player, { clock }),
  );

  assert.deepEqual(result, {
    status: 'CAPTURE_TIMEOUT',
    formats: [],
    resourceUrls: [],
  });
  assert.equal(clock.now(), 1000);
  assert.equal(playCalls, 0);
});

test('waits for the highest eligible adaptive video instead of a low-resolution pair', async () => {
  const clock = createClock();
  const lowVideoUrl = directUrl(136);
  const highVideoUrl = directUrl(137);
  const audioUrl = directUrl(140);
  const player = {
    getPlayerResponse() {
      return okResponse([
        videoFormat(),
        videoFormat({
          itag: 136,
          width: 1280,
          height: 720,
        }),
        audioFormat(),
      ]);
    },
    getAvailableQualityLevels() {
      return [];
    },
    setPlaybackQualityRange() {},
    playVideo() {},
    pauseVideo() {},
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 1000 },
    createEnvironment(player, {
      clock,
      performance: {
        getEntriesByType() {
          const entries = [
            { name: lowVideoUrl },
            { name: audioUrl },
          ];
          if (clock.now() >= 250) {
            entries.push({ name: highVideoUrl });
          }
          return entries;
        },
      },
    }),
  );

  assert.equal(result.status, 'OK');
  assert.deepEqual(clock.sleeps, [250]);
  assert.deepEqual(result.resourceUrls, [
    lowVideoUrl,
    audioUrl,
    highVideoUrl,
  ]);
});

test('gives eligible 1080p metadata half a short timeout before progressive fallback', async () => {
  const clock = createClock();
  let pauseCalls = 0;
  const player = {
    getPlayerResponse() {
      return okResponse(
        [videoFormat(), audioFormat()],
        [progressiveFormat({ url: directUrl(22) })],
      );
    },
    setPlaybackQualityRange() {},
    playVideo() {},
    pauseVideo() {
      pauseCalls += 1;
    },
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 1000 },
    createEnvironment(player, { clock }),
  );

  assert.equal(result.status, 'OK');
  assert.equal(clock.now(), 500);
  assert.deepEqual(clock.sleeps, [250, 250]);
  assert.equal(pauseCalls, 1);
});

test('accepts a covered 720p adaptive fallback after a four-second grace', async () => {
  const clock = createClock();
  const lowVideoUrl = directUrl(136);
  const audioUrl = directUrl(140);
  const player = {
    getPlayerResponse() {
      return okResponse([
        videoFormat(),
        videoFormat({
          itag: 136,
          width: 1280,
          height: 720,
        }),
        audioFormat(),
      ]);
    },
    setPlaybackQualityRange() {},
    playVideo() {},
    pauseVideo() {},
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 15_000 },
    createEnvironment(player, {
      clock,
      performance: {
        getEntriesByType() {
          return [{ name: lowVideoUrl }, { name: audioUrl }];
        },
      },
    }),
  );

  assert.equal(result.status, 'OK');
  assert.equal(clock.now(), 4000);
  assert.deepEqual(result.resourceUrls, [lowVideoUrl, audioUrl]);
});

test('returns the exact timeout envelope when no usable format is covered', async () => {
  const clock = createClock();
  let pauseCalls = 0;
  const player = {
    getPlayerResponse() {
      return okResponse([videoFormat(), audioFormat()]);
    },
    setPlaybackQualityRange() {},
    playVideo() {},
    pauseVideo() {
      pauseCalls += 1;
    },
  };

  const result = await captureYouTubePageState(
    { timeoutMs: 1000 },
    createEnvironment(player, { clock }),
  );

  assert.deepEqual(result, {
    status: 'CAPTURE_TIMEOUT',
    formats: [],
    resourceUrls: [],
  });
  assert.equal(clock.now(), 1000);
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

test('whitelists returned playability statuses and rejects oversized transient statuses', async (t) => {
  const forbidden = () => {
    throw new Error('playback method must not be touched');
  };

  await t.test('unknown status', async () => {
    const player = {
      getPlayerResponse() {
        return {
          playabilityStatus: {
            status: 'PRIVATE_INTERNAL_STATUS',
            reason: 'private account reason',
          },
        };
      },
      mute: forbidden,
      playVideo: forbidden,
      pauseVideo: forbidden,
    };

    assert.deepEqual(
      await captureYouTubePageState({}, createEnvironment(player)),
      {
        status: 'UNPLAYABLE',
        formats: [],
        resourceUrls: [],
      },
    );
  });

  await t.test('oversized status', async () => {
    const clock = createClock();
    const player = {
      getPlayerResponse() {
        return {
          playabilityStatus: {
            status: 'X'.repeat(513),
          },
        };
      },
      mute: forbidden,
      playVideo: forbidden,
      pauseVideo: forbidden,
    };

    assert.deepEqual(
      await captureYouTubePageState(
        { timeoutMs: 1000 },
        createEnvironment(player, { clock }),
      ),
      {
        status: 'CAPTURE_TIMEOUT',
        formats: [],
        resourceUrls: [],
      },
    );
    assert.equal(clock.now(), 1000);
  });
});

test('omits oversized copied strings and resource URLs', async () => {
  const oversizedDirectUrl = directUrl(
    999,
    `&private=${'x'.repeat(16_384)}`,
  );
  const oversizedResourceUrl = directUrl(
    22,
    `&private=${'y'.repeat(16_384)}`,
  );
  const player = {
    getPlayerResponse() {
      return okResponse(
        [
          videoFormat({
            itag: 999,
            mimeType: 'v'.repeat(513),
            audioQuality: 'a'.repeat(513),
            url: oversizedDirectUrl,
          }),
        ],
        [progressiveFormat({ url: directUrl(22) })],
      );
    },
    playVideo() {},
    pauseVideo() {},
  };

  const result = await captureYouTubePageState(
    {},
    createEnvironment(player, {
      performance: {
        getEntriesByType() {
          return [{ name: oversizedResourceUrl }];
        },
      },
    }),
  );
  const boundedFormat = result.formats.find(
    (format) => format.itag === 999,
  );

  assert.equal(result.status, 'OK');
  assert.equal(Object.hasOwn(boundedFormat, 'mimeType'), false);
  assert.equal(Object.hasOwn(boundedFormat, 'audioQuality'), false);
  assert.equal(Object.hasOwn(boundedFormat, 'url'), false);
  assert.deepEqual(result.resourceUrls, []);
});

test('omits oversized or nonnumeric scalar and range values', async () => {
  const oversizedDigits = '9'.repeat(1_000_000);
  const player = {
    getPlayerResponse() {
      return okResponse(
        [
          videoFormat({
            itag: 999,
            width: oversizedDigits,
            approxDurationMs: oversizedDigits,
            initRange: {
              start: oversizedDigits,
              end: '739',
            },
            indexRange: {
              start: 'not-numeric',
              end: 1200,
            },
          }),
        ],
        [progressiveFormat({ url: directUrl(22) })],
      );
    },
    playVideo() {},
    pauseVideo() {},
  };

  const result = await captureYouTubePageState(
    {},
    createEnvironment(player),
  );
  const boundedFormat = result.formats.find(
    (format) => format.itag === 999,
  );

  assert.equal(result.status, 'OK');
  assert.equal(Object.hasOwn(boundedFormat, 'width'), false);
  assert.equal(Object.hasOwn(boundedFormat, 'approxDurationMs'), false);
  assert.deepEqual(boundedFormat.initRange, { end: '739' });
  assert.deepEqual(boundedFormat.indexRange, { end: 1200 });
  assert.equal(JSON.stringify(result).length < 20_000, true);
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
      return okResponse(
        [audioFormat()],
        [progressiveFormat()],
      );
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
  assert.notEqual(
    result.formats[0].indexRange,
    response.streamingData.adaptiveFormats[0].indexRange,
  );
  result.formats[0].initRange.start = 'changed';
  assert.equal(
    response.streamingData.adaptiveFormats[0].initRange.start,
    '0',
  );
  response.streamingData.adaptiveFormats[0].indexRange.start =
    'raw-changed';
  assert.equal(result.formats[0].indexRange.start, '740');
});

test('omits array, function, primitive, and null range values', async (t) => {
  for (const scenario of [
    { name: 'array', value: ['private', 'range'] },
    { name: 'function', value() {} },
    { name: 'primitive', value: '0-739' },
    { name: 'null', value: null },
  ]) {
    await t.test(scenario.name, async () => {
      const player = {
        getPlayerResponse() {
          return okResponse([], [
            progressiveFormat({
              initRange: scenario.value,
              indexRange: scenario.value,
              url: directUrl(22),
            }),
          ]);
        },
        playVideo() {},
        pauseVideo() {},
      };

      const result = await captureYouTubePageState(
        {},
        createEnvironment(player),
      );

      assert.equal(Object.hasOwn(result.formats[0], 'initRange'), false);
      assert.equal(Object.hasOwn(result.formats[0], 'indexRange'), false);
    });
  }
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
