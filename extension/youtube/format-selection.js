(function (root, factory) {
  'use strict';

  const api = factory();

  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  } else {
    root.__rssPalYouTubeFormatSelection = api;
  }
})(globalThis, function () {
  'use strict';

  const REQUEST_LOCAL_QUERY_KEYS = ['range', 'rn', 'rbuf', 'ump', 'alr'];
  const MAX_MEDIA_URL_LENGTH = 16_384;
  const MAX_MIME_LENGTH = 128;
  const MAX_CODECS_LENGTH = 256;
  const MAX_MEDIA_RANGE_OFFSET = 64 * 1024 * 1024 - 1;
  const MAX_MEDIA_RANGE_SPAN = 16 * 1024 * 1024;

  function parseTrustedGoogleVideoUrl(value) {
    if (
      typeof value !== 'string' ||
      value.length > MAX_MEDIA_URL_LENGTH
    ) {
      return null;
    }

    try {
      const url = new URL(value);
      const trustedHost =
        url.hostname === 'googlevideo.com' ||
        url.hostname.endsWith('.googlevideo.com');

      if (
        url.protocol !== 'https:' ||
        !trustedHost ||
        url.username !== '' ||
        url.password !== '' ||
        (
          url.pathname !== '/videoplayback' &&
          !url.pathname.startsWith('/videoplayback/')
        )
      ) {
        return null;
      }

      return url;
    } catch {
      return null;
    }
  }

  function parsePositiveInteger(value) {
    if (typeof value === 'number') {
      return Number.isSafeInteger(value) && value > 0 ? value : null;
    }

    if (typeof value !== 'string' || !/^[1-9]\d*$/.test(value)) {
      return null;
    }

    const parsed = Number(value);
    return Number.isSafeInteger(parsed) ? parsed : null;
  }

  function parseNonnegativeInteger(value) {
    if (typeof value === 'number') {
      return Number.isSafeInteger(value) && value >= 0 ? value : null;
    }

    if (typeof value !== 'string' || !/^\d+$/.test(value)) {
      return null;
    }

    const parsed = Number(value);
    return Number.isSafeInteger(parsed) ? parsed : null;
  }

  function parsePathValue(pathname, name) {
    const match = pathname.match(
      new RegExp(`/${name}/([^/]+)(?:/|$)`, 'i'),
    );
    return match ? match[1] : null;
  }

  function normalizeGoogleVideoUrl(value) {
    const url = parseTrustedGoogleVideoUrl(value);
    if (url === null) {
      return null;
    }

    for (const key of REQUEST_LOCAL_QUERY_KEYS) {
      url.searchParams.delete(key);
    }

    return url.toString();
  }

  function parseItag(value) {
    const url = parseTrustedGoogleVideoUrl(value);
    if (url === null) {
      return null;
    }

    const queryItag = parsePositiveInteger(url.searchParams.get('itag'));
    if (queryItag !== null) {
      return queryItag;
    }

    return parsePositiveInteger(parsePathValue(url.pathname, 'itag'));
  }

  function parseMimeType(value) {
    if (typeof value !== 'string') {
      return null;
    }

    const match = value.match(
      /^\s*((?:video|audio)\/[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]*)\s*;\s*codecs="([^"]*)"\s*$/i,
    );
    if (match === null) {
      return null;
    }
    if (match[1].length > MAX_MIME_LENGTH) {
      return null;
    }

    return {
      mimeType: match[1].toLowerCase(),
      codecs: match[2],
    };
  }

  function sanitizeRange(value) {
    if (value === null || typeof value !== 'object' || Array.isArray(value)) {
      return null;
    }

    const start = parseNonnegativeInteger(value.start);
    const end = parseNonnegativeInteger(value.end);
    if (
      start === null ||
      end === null ||
      start > MAX_MEDIA_RANGE_OFFSET ||
      end > MAX_MEDIA_RANGE_OFFSET ||
      end < start ||
      end - start + 1 > MAX_MEDIA_RANGE_SPAN
    ) {
      return null;
    }

    return { start, end };
  }

  function copyPositiveInteger(target, targetKey, source, sourceKey) {
    const parsed = parsePositiveInteger(source[sourceKey]);
    if (parsed !== null) {
      target[targetKey] = parsed;
    }
  }

  function sanitizeFormat(raw) {
    if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) {
      return null;
    }

    const itag = parsePositiveInteger(raw.itag);
    const mime = parseMimeType(raw.mimeType);
    if (itag === null || mime === null) {
      return null;
    }

    const sanitized = {
      itag,
      mimeType: mime.mimeType,
      codecs: mime.codecs,
    };

    copyPositiveInteger(sanitized, 'bitrate', raw, 'bitrate');
    copyPositiveInteger(sanitized, 'durationMs', raw, 'approxDurationMs');
    copyPositiveInteger(sanitized, 'width', raw, 'width');
    copyPositiveInteger(sanitized, 'height', raw, 'height');
    copyPositiveInteger(sanitized, 'frameRate', raw, 'fps');
    copyPositiveInteger(
      sanitized,
      'audioSampleRate',
      raw,
      'audioSampleRate',
    );
    copyPositiveInteger(sanitized, 'audioChannels', raw, 'audioChannels');

    const initRange = sanitizeRange(raw.initRange);
    if (initRange !== null) {
      sanitized.initRange = initRange;
    }

    const indexRange = sanitizeRange(raw.indexRange);
    if (indexRange !== null) {
      sanitized.indexRange = indexRange;
    }

    sanitized.hasAudio = Boolean(raw.audioQuality);

    const url = normalizeGoogleVideoUrl(raw.url);
    if (url !== null && parseItag(url) === itag) {
      sanitized.url = url;
    }

    return sanitized;
  }

  function inspectExpiry(value, nowMs) {
    const url = parseTrustedGoogleVideoUrl(value);
    if (url === null) {
      return { usable: false, expiresAtMs: null };
    }

    let rawExpiry = null;
    let hasExpiry = false;
    if (url.searchParams.has('expire')) {
      hasExpiry = true;
      rawExpiry = url.searchParams.get('expire');
    } else {
      rawExpiry = parsePathValue(url.pathname, 'expire');
      hasExpiry = rawExpiry !== null;
    }

    if (!hasExpiry) {
      return { usable: true, expiresAtMs: nowMs + 300_000 };
    }

    const expirySeconds = parsePositiveInteger(rawExpiry);
    if (expirySeconds === null) {
      return { usable: false, expiresAtMs: null };
    }

    const expiresAtMs = expirySeconds * 1000;
    if (
      !Number.isSafeInteger(expiresAtMs) ||
      !Number.isFinite(new Date(expiresAtMs).getTime()) ||
      expiresAtMs <= nowMs
    ) {
      return { usable: false, expiresAtMs: null };
    }

    return { usable: true, expiresAtMs };
  }

  function hasAdaptiveRanges(format) {
    return (
      format.initRange !== undefined && format.indexRange !== undefined
    );
  }

  function hasAdaptivePlaybackMetadata(format) {
    return (
      Number.isFinite(format.bitrate) &&
      format.bitrate > 0 &&
      Number.isFinite(format.durationMs) &&
      format.durationMs > 0 &&
      typeof format.codecs === 'string' &&
      format.codecs.length > 0 &&
      format.codecs.length <= MAX_CODECS_LENGTH
    );
  }

  function preferMp4(left, right, mediaType) {
    const expectedMime = `${mediaType}/mp4`;
    return Number(right.mimeType === expectedMime) -
      Number(left.mimeType === expectedMime);
  }

  function compareBitrate(left, right) {
    return (right.bitrate || 0) - (left.bitrate || 0);
  }

  function compareAdaptiveVideo(left, right) {
    const heightDifference = right.height - left.height;
    if (heightDifference !== 0) {
      return heightDifference;
    }

    const leftAtMost30 =
      Number.isInteger(left.frameRate) && left.frameRate <= 30;
    const rightAtMost30 =
      Number.isInteger(right.frameRate) && right.frameRate <= 30;
    if (leftAtMost30 !== rightAtMost30) {
      return Number(rightAtMost30) - Number(leftAtMost30);
    }

    return (
      preferMp4(left, right, 'video') || compareBitrate(left, right)
    );
  }

  function compareAdaptiveAudio(left, right) {
    return (
      preferMp4(left, right, 'audio') || compareBitrate(left, right)
    );
  }

  function compareProgressive(left, right) {
    return (
      right.height - left.height ||
      preferMp4(left, right, 'video') ||
      compareBitrate(left, right)
    );
  }

  function makeVideoTrack(format) {
    const track = {
      url: format.url,
      mimeType: format.mimeType,
      codecs: format.codecs,
      bitrate: format.bitrate,
      initRange: format.initRange,
      indexRange: format.indexRange,
      durationMs: format.durationMs,
      width: format.width,
      height: format.height,
    };

    if (format.frameRate !== undefined) {
      track.frameRate = format.frameRate;
    }

    return track;
  }

  function makeAudioTrack(format) {
    const track = {
      url: format.url,
      mimeType: format.mimeType,
      codecs: format.codecs,
      bitrate: format.bitrate,
      initRange: format.initRange,
      indexRange: format.indexRange,
      durationMs: format.durationMs,
    };

    if (format.audioSampleRate !== undefined) {
      track.audioSampleRate = format.audioSampleRate;
    }

    return track;
  }

  function makeProgressiveTrack(format) {
    return {
      url: format.url,
      mimeType: format.mimeType,
      height: format.height,
    };
  }

  function playbackExpiresAt(selectedFormats, nowMs) {
    const selectedExpiries = selectedFormats
      .map((format) => format.expiresAtMs)
      .filter((value) => value !== null);
    const expiresAtMs =
      selectedExpiries.length > 0
        ? Math.min(...selectedExpiries)
        : nowMs + 300_000;

    return new Date(expiresAtMs).toISOString();
  }

  function selectPlayback(captured, nowMs) {
    if (captured && captured.status === 'LOGIN_REQUIRED') {
      return { ok: false, code: 'LOGIN_REQUIRED' };
    }
    if (!captured || captured.status !== 'OK') {
      return { ok: false, code: 'VIDEO_UNAVAILABLE' };
    }

    const currentTime = Number.isFinite(nowMs) ? nowMs : Date.now();
    const formats = Array.isArray(captured.formats)
      ? captured.formats.map(sanitizeFormat).filter(Boolean)
      : [];
    const urlsByItag = new Map();

    for (const format of formats) {
      if (format.url !== undefined) {
        urlsByItag.set(format.itag, format.url);
      }
    }

    if (Array.isArray(captured.resourceUrls)) {
      for (const resourceUrl of captured.resourceUrls) {
        const url = normalizeGoogleVideoUrl(resourceUrl);
        const itag = url === null ? null : parseItag(url);
        if (url !== null && itag !== null) {
          urlsByItag.set(itag, url);
        }
      }
    }

    const usableFormats = [];
    for (const format of formats) {
      const url = urlsByItag.get(format.itag);
      if (url === undefined) {
        continue;
      }

      const expiry = inspectExpiry(url, currentTime);
      if (!expiry.usable) {
        continue;
      }

      usableFormats.push({
        ...format,
        url,
        expiresAtMs: expiry.expiresAtMs,
      });
    }

    const adaptiveVideos = usableFormats
      .filter(
        (format) =>
          format.mimeType.startsWith('video/') &&
          !format.hasAudio &&
          hasAdaptiveRanges(format) &&
          hasAdaptivePlaybackMetadata(format) &&
          Number.isInteger(format.height) &&
          format.height >= 720 &&
          format.height <= 1080,
      )
      .sort(compareAdaptiveVideo);
    const adaptiveAudios = usableFormats
      .filter(
        (format) =>
          format.mimeType.startsWith('audio/') &&
          hasAdaptiveRanges(format) &&
          hasAdaptivePlaybackMetadata(format),
      )
      .sort(compareAdaptiveAudio);
    const progressiveFormats = usableFormats
      .filter(
        (format) =>
          format.mimeType.startsWith('video/') &&
          format.hasAudio === true &&
          Number.isInteger(format.height) &&
          format.height > 0 &&
          format.height <= 720,
      )
      .sort(compareProgressive);

    if (adaptiveVideos.length > 0 && adaptiveAudios.length > 0) {
      const video = adaptiveVideos[0];
      const audio = adaptiveAudios[0];
      const progressive = progressiveFormats[0];
      const selectedFormats = [video, audio];
      if (progressive !== undefined) {
        selectedFormats.push(progressive);
      }

      const playback = {
        mode: 'dash',
        quality: video.height,
        expiresAt: playbackExpiresAt(selectedFormats, currentTime),
        video: makeVideoTrack(video),
        audio: makeAudioTrack(audio),
      };

      if (progressive !== undefined) {
        playback.progressive = makeProgressiveTrack(progressive);
      }

      return {
        ok: true,
        playback,
      };
    }

    if (progressiveFormats.length > 0) {
      const progressive = progressiveFormats[0];
      return {
        ok: true,
        playback: {
          mode: 'progressive',
          quality: progressive.height,
          expiresAt: playbackExpiresAt([progressive], currentTime),
          progressive: makeProgressiveTrack(progressive),
        },
      };
    }

    return { ok: false, code: 'NO_SUPPORTED_FORMAT' };
  }

  return Object.freeze({
    normalizeGoogleVideoUrl,
    parseItag,
    sanitizeFormat,
    selectPlayback,
  });
});
