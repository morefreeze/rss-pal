'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const formatSelection = require('./format-selection');
const {
  normalizeGoogleVideoUrl,
  parseItag,
  sanitizeFormat,
  selectPlayback,
} = formatSelection;

const NOW_MS = 1_770_000_000_000;
const DEFAULT_EXPIRE_SECONDS = 1_780_000_600;

function googleVideoUrl(
  itag,
  {
    expire = DEFAULT_EXPIRE_SECONDS,
    host = 'rr1---sn-a5mekn6z.googlevideo.com',
    extra = '',
  } = {},
) {
  const expireQuery = expire === null ? '' : `expire=${expire}&`;
  return `https://${host}/videoplayback?${expireQuery}itag=${itag}&sig=sig-${itag}${extra}`;
}

function videoFormat(
  itag,
  height,
  {
    mimeType = 'video/mp4; codecs="avc1.640028"',
    bitrate = 4_500_000,
    fps = 30,
    url = googleVideoUrl(itag),
    initRange = { start: '0', end: '739' },
    indexRange = { start: '740', end: '1200' },
  } = {},
) {
  return {
    itag,
    mimeType,
    bitrate,
    approxDurationMs: '600000',
    width: height === 1080 ? 1920 : 1280,
    height,
    fps,
    initRange,
    indexRange,
    url,
  };
}

function audioFormat(
  {
    itag = 140,
    url = googleVideoUrl(itag),
    mimeType = 'audio/mp4; codecs="mp4a.40.2"',
    bitrate = 129_000,
    initRange = { start: '0', end: '721' },
    indexRange = { start: '722', end: '1050' },
  } = {},
) {
  return {
    itag,
    mimeType,
    bitrate,
    approxDurationMs: '600000',
    audioSampleRate: '48000',
    audioChannels: 2,
    initRange,
    indexRange,
    url,
  };
}

function progressiveFormat(
  {
    height = 720,
    url = googleVideoUrl(22),
    mimeType = 'video/mp4; codecs="avc1.64001F, mp4a.40.2"',
    bitrate = 2_500_000,
  } = {},
) {
  return {
    itag: 22,
    mimeType,
    bitrate,
    approxDurationMs: '600000',
    width: 1280,
    height,
    fps: 30,
    audioQuality: 'AUDIO_QUALITY_MEDIUM',
    url,
  };
}

function captured(formats, resourceUrls = []) {
  return {
    status: 'OK',
    formats,
    resourceUrls,
  };
}

function deepFreeze(value) {
  if (value && typeof value === 'object' && !Object.isFrozen(value)) {
    Object.freeze(value);
    for (const child of Object.values(value)) {
      deepFreeze(child);
    }
  }
  return value;
}

test('exports only the frozen format-selection API through CommonJS and the browser global', () => {
  assert.deepEqual(Object.keys(formatSelection), [
    'normalizeGoogleVideoUrl',
    'parseItag',
    'sanitizeFormat',
    'selectPlayback',
  ]);
  assert.equal(Object.isFrozen(formatSelection), true);
  assert.equal(
    Object.values(formatSelection).every((value) => typeof value === 'function'),
    true,
  );

  const source = fs.readFileSync(require.resolve('./format-selection'), 'utf8');
  const context = vm.createContext({ URL });
  vm.runInContext(source, context);

  assert.equal(Object.isFrozen(context.__rssPalYouTubeFormatSelection), true);
  assert.deepEqual(
    [...Object.keys(context.__rssPalYouTubeFormatSelection)],
    Object.keys(formatSelection),
  );
  assert.equal(context.formatSelection, undefined);
  assert.equal(context.GOOGLE_VIDEO_HOST_RE, undefined);
});

test('parses positive itags from query or path only on trusted GoogleVideo URLs', () => {
  assert.equal(
    parseItag(
      'https://rr1---sn-a5mekn6z.googlevideo.com/videoplayback?itag=137',
    ),
    137,
  );
  assert.equal(
    parseItag(
      'https://googlevideo.com/videoplayback/itag/140/expire/1780000600/',
    ),
    140,
  );

  for (const url of [
    'http://googlevideo.com/videoplayback?itag=137',
    'https://googlevideo.com.evil/videoplayback?itag=137',
    'https://evilgooglevideo.com/videoplayback?itag=137',
    'https://googlevideo.com/not-media?itag=137',
    'https://googlevideo.com/videoplayback?itag=0',
    'https://googlevideo.com/videoplayback?itag=-137',
    'https://googlevideo.com/videoplayback?itag=13.7',
    'not a URL',
  ]) {
    assert.equal(parseItag(url), null, url);
  }
});

test('normalizes GoogleVideo URLs by removing only request-local query keys', () => {
  const raw =
    'https://rr1---sn-a5mekn6z.googlevideo.com/videoplayback?' +
    'expire=1780000600&itag=137&range=0-999&rn=1&rbuf=123&ump=1&alr=yes&' +
    'sig=abc&lsig=def&n=ghi&pot=jkl';

  assert.equal(
    normalizeGoogleVideoUrl(raw),
    'https://rr1---sn-a5mekn6z.googlevideo.com/videoplayback?' +
      'expire=1780000600&itag=137&sig=abc&lsig=def&n=ghi&pot=jkl',
  );

  for (const url of [
    'http://googlevideo.com/videoplayback?itag=137',
    'https://googlevideo.com.evil/videoplayback?itag=137',
    'https://evilgooglevideo.com/videoplayback?itag=137',
    'https://googlevideo.com/player?itag=137',
    'not a URL',
  ]) {
    assert.equal(normalizeGoogleVideoUrl(url), null, url);
  }
});

test('sanitizes known format fields and strips cipher, headers, reasons, and unknown data', () => {
  const raw = {
    itag: '140',
    mimeType: 'audio/mp4; codecs="mp4a.40.2"',
    bitrate: '129000',
    approxDurationMs: '600000',
    width: '-1',
    height: '720',
    fps: '30.5',
    audioSampleRate: '48000',
    audioChannels: '9007199254740992',
    initRange: { start: '0', end: '721' },
    indexRange: { start: 800, end: 799 },
    audioQuality: 'AUDIO_QUALITY_MEDIUM',
    url: `${googleVideoUrl(140)}&range=0-721&rn=7`,
    signatureCipher: 's=private-signature',
    reason: 'private reason',
    headers: { Cookie: 'private' },
    unknown: { nested: true },
  };
  const snapshot = structuredClone(raw);

  assert.deepEqual(sanitizeFormat(raw), {
    itag: 140,
    mimeType: 'audio/mp4',
    codecs: 'mp4a.40.2',
    bitrate: 129000,
    durationMs: 600000,
    height: 720,
    audioSampleRate: 48000,
    initRange: { start: 0, end: 721 },
    hasAudio: true,
    url: googleVideoUrl(140),
  });
  assert.deepEqual(raw, snapshot);
});

test('rejects formats without a positive itag and an audio or video MIME with codecs', () => {
  for (const raw of [
    null,
    {},
    { itag: 0, mimeType: 'video/mp4; codecs="avc1"' },
    { itag: -1, mimeType: 'video/mp4; codecs="avc1"' },
    { itag: 137, mimeType: 'video/mp4' },
    { itag: 137, mimeType: 'text/plain; codecs="utf-8"' },
    { itag: 137, mimeType: 'video/mp4; codecs=avc1' },
  ]) {
    assert.equal(sanitizeFormat(raw), null);
  }

  assert.deepEqual(
    sanitizeFormat({
      itag: 137,
      mimeType: 'video/mp4; codecs=""',
    }),
    {
      itag: 137,
      mimeType: 'video/mp4',
      codecs: '',
      hasAudio: false,
    },
  );
});

test('uses observed resource URLs as the source of truth for each itag', () => {
  const observedVideo =
    'https://rr2---sn-a5mekn6z.googlevideo.com/videoplayback?' +
    'expire=1780000600&itag=137&sig=observed-video&range=0-999&rn=9';
  const observedAudio =
    'https://rr2---sn-a5mekn6z.googlevideo.com/videoplayback?' +
    'expire=1780000600&itag=140&sig=observed-audio&rbuf=10';

  const result = selectPlayback(
    captured(
      [
        videoFormat(137, 1080, {
          url: `${googleVideoUrl(137)}&source=direct-video`,
        }),
        audioFormat({
          url: `${googleVideoUrl(140)}&source=direct-audio`,
        }),
      ],
      [observedVideo, observedAudio],
    ),
    NOW_MS,
  );

  assert.equal(result.ok, true);
  assert.equal(
    result.playback.video.url,
    'https://rr2---sn-a5mekn6z.googlevideo.com/videoplayback?' +
      'expire=1780000600&itag=137&sig=observed-video',
  );
  assert.equal(
    result.playback.audio.url,
    'https://rr2---sn-a5mekn6z.googlevideo.com/videoplayback?' +
      'expire=1780000600&itag=140&sig=observed-audio',
  );
});

test('prefers 1080p at 30 fps over 1080p60 and 720p and reports truthful quality', () => {
  const result = selectPlayback(
    captured([
      videoFormat(136, 720, { bitrate: 3_000_000 }),
      videoFormat(399, 1080, {
        mimeType: 'video/webm; codecs="av01.0.08M.08"',
        bitrate: 8_000_000,
        fps: 60,
      }),
      videoFormat(137, 1080, { bitrate: 4_500_000, fps: 30 }),
      audioFormat(),
    ]),
    NOW_MS,
  );

  assert.equal(result.ok, true);
  assert.equal(result.playback.mode, 'dash');
  assert.equal(result.playback.quality, 1080);
  assert.equal(result.playback.video.height, 1080);
  assert.match(result.playback.video.url, /[?&]itag=137(?:&|$)/);
});

test('returns an exact usable 720p progressive fallback with a 1080p DASH pair', () => {
  const fallbackUrl = googleVideoUrl(22, {
    extra: '&range=0-999&rn=7&pot=po-token&n=throttle-token',
  });
  const result = selectPlayback(
    captured([
      videoFormat(137, 1080),
      audioFormat(),
      progressiveFormat({ url: fallbackUrl }),
    ]),
    NOW_MS,
  );

  assert.equal(result.ok, true);
  assert.deepEqual(result.playback, {
    mode: 'dash',
    quality: 1080,
    expiresAt: new Date(DEFAULT_EXPIRE_SECONDS * 1000).toISOString(),
    video: {
      url: googleVideoUrl(137),
      mimeType: 'video/mp4',
      codecs: 'avc1.640028',
      bitrate: 4_500_000,
      initRange: { start: 0, end: 739 },
      indexRange: { start: 740, end: 1200 },
      durationMs: 600_000,
      width: 1920,
      height: 1080,
      frameRate: 30,
    },
    audio: {
      url: googleVideoUrl(140),
      mimeType: 'audio/mp4',
      codecs: 'mp4a.40.2',
      bitrate: 129_000,
      initRange: { start: 0, end: 721 },
      indexRange: { start: 722, end: 1050 },
      durationMs: 600_000,
      audioSampleRate: 48_000,
    },
    progressive: {
      url:
        `${googleVideoUrl(22)}&pot=po-token&n=throttle-token`,
      mimeType: 'video/mp4',
      height: 720,
    },
  });
});

test('omits the DASH progressive fallback when no finalized URL is usable', () => {
  const unavailableFallback = progressiveFormat();
  delete unavailableFallback.url;

  const result = selectPlayback(
    captured([
      videoFormat(137, 1080),
      audioFormat(),
      unavailableFallback,
    ]),
    NOW_MS,
  );

  assert.equal(result.ok, true);
  assert.equal(result.playback.mode, 'dash');
  assert.equal(
    Object.prototype.hasOwnProperty.call(result.playback, 'progressive'),
    false,
  );
});

test('bounds a DASH contract by the progressive fallback URL expiry', () => {
  const fallbackExpiry = Math.floor(NOW_MS / 1000) + 180;
  const result = selectPlayback(
    captured([
      videoFormat(137, 1080, {
        url: googleVideoUrl(137, { expire: fallbackExpiry + 600 }),
      }),
      audioFormat({
        url: googleVideoUrl(140, { expire: fallbackExpiry + 300 }),
      }),
      progressiveFormat({
        url: googleVideoUrl(22, { expire: fallbackExpiry }),
      }),
    ]),
    NOW_MS,
  );

  assert.equal(result.ok, true);
  assert.equal(
    result.playback.expiresAt,
    new Date(fallbackExpiry * 1000).toISOString(),
  );
});

test('falls back to a complete 720p adaptive pair', () => {
  const result = selectPlayback(
    captured([
      videoFormat(137, 1080, { indexRange: null }),
      videoFormat(136, 720),
      audioFormat(),
    ]),
    NOW_MS,
  );

  assert.equal(result.ok, true);
  assert.equal(result.playback.mode, 'dash');
  assert.equal(result.playback.quality, 720);
  assert.equal(result.playback.video.height, 720);
  assert.match(result.playback.video.url, /[?&]itag=136(?:&|$)/);
});

test('skips adaptive video and audio tracks without positive bitrate and duration', () => {
  const invalidMetadataCases = [
    {
      name: 'missing bitrate',
      mutate(format) {
        delete format.bitrate;
      },
    },
    {
      name: 'zero bitrate',
      mutate(format) {
        format.bitrate = 0;
      },
    },
    {
      name: 'missing duration',
      mutate(format) {
        delete format.approxDurationMs;
      },
    },
    {
      name: 'zero duration',
      mutate(format) {
        format.approxDurationMs = 0;
      },
    },
  ];

  for (const metadataCase of invalidMetadataCases) {
    const invalidVideo = videoFormat(137, 1080);
    metadataCase.mutate(invalidVideo);
    const videoFallback = selectPlayback(
      captured([invalidVideo, videoFormat(136, 720), audioFormat()]),
      NOW_MS,
    );

    assert.equal(videoFallback.ok, true, metadataCase.name);
    assert.equal(videoFallback.playback.quality, 720, metadataCase.name);
    assert.match(
      videoFallback.playback.video.url,
      /[?&]itag=136(?:&|$)/,
      metadataCase.name,
    );

    const invalidAudio = audioFormat();
    metadataCase.mutate(invalidAudio);
    const audioFallback = selectPlayback(
      captured([
        videoFormat(137, 1080),
        invalidAudio,
        audioFormat({
          itag: 251,
          url: googleVideoUrl(251),
          mimeType: 'audio/webm; codecs="opus"',
          bitrate: 120_000,
        }),
      ]),
      NOW_MS,
    );

    assert.equal(audioFallback.ok, true, metadataCase.name);
    assert.match(
      audioFallback.playback.audio.url,
      /[?&]itag=251(?:&|$)/,
      metadataCase.name,
    );

    for (const track of [
      videoFallback.playback.video,
      videoFallback.playback.audio,
      audioFallback.playback.video,
      audioFallback.playback.audio,
    ]) {
      assert.equal(
        Number.isFinite(track.bitrate) && track.bitrate > 0,
        true,
        `${metadataCase.name}: bitrate`,
      );
      assert.equal(
        Number.isFinite(track.durationMs) && track.durationMs > 0,
        true,
        `${metadataCase.name}: durationMs`,
      );
    }
  }
});

test('returns no supported format instead of serializing undefined adaptive metadata', () => {
  const video = videoFormat(137, 1080);
  delete video.bitrate;
  const audio = audioFormat();
  audio.approxDurationMs = 0;

  assert.deepEqual(selectPlayback(captured([video, audio]), NOW_MS), {
    ok: false,
    code: 'NO_SUPPORTED_FORMAT',
  });
});

test('uses a progressive 720p format when no complete adaptive pair is usable', () => {
  const result = selectPlayback(
    captured([
      videoFormat(137, 1080),
      audioFormat({ initRange: { start: 8, end: 7 } }),
      progressiveFormat(),
    ]),
    NOW_MS,
  );

  assert.deepEqual(result, {
    ok: true,
    playback: {
      mode: 'progressive',
      quality: 720,
      expiresAt: new Date(DEFAULT_EXPIRE_SECONDS * 1000).toISOString(),
      progressive: {
        url: googleVideoUrl(22),
        mimeType: 'video/mp4',
        height: 720,
      },
    },
  });
});

test('falls back around invalid, expired, and incomplete adaptive candidates', () => {
  const expiredSeconds = Math.floor(NOW_MS / 1000) - 1;
  const result = selectPlayback(
    captured([
      videoFormat(137, 1080, {
        url: googleVideoUrl(137, { expire: expiredSeconds }),
      }),
      videoFormat(136, 720, {
        url: 'https://googlevideo.com.evil/videoplayback?itag=136',
      }),
      audioFormat({ indexRange: undefined }),
      progressiveFormat({ height: 360 }),
    ]),
    NOW_MS,
  );

  assert.equal(result.ok, true);
  assert.equal(result.playback.mode, 'progressive');
  assert.equal(result.playback.quality, 360);

  assert.deepEqual(
    selectPlayback(
      captured([
        videoFormat(137, 1080, {
          url: googleVideoUrl(137, { expire: expiredSeconds }),
        }),
        videoFormat(136, 720, { initRange: null }),
        audioFormat(),
      ]),
      NOW_MS,
    ),
    { ok: false, code: 'NO_SUPPORTED_FORMAT' },
  );
});

test('rejects invalid expiry values instead of granting them fallback validity', () => {
  assert.deepEqual(
    selectPlayback(
      captured([
        videoFormat(137, 1080, {
          url: googleVideoUrl(137, { expire: 'not-a-time' }),
        }),
        audioFormat(),
      ]),
      NOW_MS,
    ),
    { ok: false, code: 'NO_SUPPORTED_FORMAT' },
  );
});

test('returns exact public failure objects without leaking captured reason text', () => {
  assert.deepEqual(
    selectPlayback(
      {
        status: 'LOGIN_REQUIRED',
        formats: [],
        resourceUrls: [],
        reason: 'private login diagnostics',
      },
      NOW_MS,
    ),
    { ok: false, code: 'LOGIN_REQUIRED' },
  );
  assert.deepEqual(
    selectPlayback(
      {
        status: 'ERROR',
        formats: [],
        resourceUrls: [],
        reason: 'private unavailable diagnostics',
      },
      NOW_MS,
    ),
    { ok: false, code: 'VIDEO_UNAVAILABLE' },
  );
  assert.deepEqual(
    selectPlayback(
      {
        status: 'OK',
        formats: [],
        resourceUrls: [],
        reason: 'private selection diagnostics',
      },
      NOW_MS,
    ),
    { ok: false, code: 'NO_SUPPORTED_FORMAT' },
  );
});

test('uses the earliest selected URL expiry from query or path', () => {
  const videoExpiry = 1_780_000_800;
  const audioExpiry = 1_780_000_400;
  const result = selectPlayback(
    captured([
      videoFormat(137, 1080, {
        url: googleVideoUrl(137, { expire: videoExpiry }),
      }),
      audioFormat({
        url:
          'https://rr1---sn-a5mekn6z.googlevideo.com/' +
          `videoplayback/itag/140/expire/${audioExpiry}/?sig=audio`,
      }),
    ]),
    NOW_MS,
  );

  assert.equal(result.ok, true);
  assert.equal(
    result.playback.expiresAt,
    new Date(audioExpiry * 1000).toISOString(),
  );
});

test('uses a five-minute expiry when selected URLs contain no expiry', () => {
  const result = selectPlayback(
    captured([
      videoFormat(137, 1080, {
        url: googleVideoUrl(137, { expire: null }),
      }),
      audioFormat({
        url: googleVideoUrl(140, { expire: null }),
      }),
    ]),
    NOW_MS,
  );

  assert.equal(result.ok, true);
  assert.equal(
    result.playback.expiresAt,
    new Date(NOW_MS + 300_000).toISOString(),
  );
});

test('gives every selected URL a five-minute effective expiry when explicit expiry is absent', () => {
  const farFutureExpiry = DEFAULT_EXPIRE_SECONDS + 10_000;
  const cases = [
    {
      videoUrl: googleVideoUrl(137, { expire: farFutureExpiry }),
      audioUrl: googleVideoUrl(140, { expire: null }),
    },
    {
      videoUrl: googleVideoUrl(137, { expire: null }),
      audioUrl:
        'https://rr1---sn-a5mekn6z.googlevideo.com/' +
        `videoplayback/itag/140/expire/${farFutureExpiry}/?sig=audio`,
    },
  ];

  for (const { videoUrl, audioUrl } of cases) {
    const result = selectPlayback(
      captured([
        videoFormat(137, 1080, { url: videoUrl }),
        audioFormat({ url: audioUrl }),
      ]),
      NOW_MS,
    );

    assert.equal(result.ok, true);
    assert.equal(
      result.playback.expiresAt,
      new Date(NOW_MS + 300_000).toISOString(),
    );
  }
});

test('does not mutate captured formats, ranges, or observed resource URLs', () => {
  const input = {
    status: 'OK',
    formats: [videoFormat(137, 1080), audioFormat()],
    resourceUrls: [
      `${googleVideoUrl(137)}&range=0-999`,
      `${googleVideoUrl(140)}&rn=8`,
    ],
  };
  const snapshot = structuredClone(input);
  deepFreeze(input);

  const result = selectPlayback(input, NOW_MS);

  assert.equal(result.ok, true);
  assert.deepEqual(input, snapshot);
});
