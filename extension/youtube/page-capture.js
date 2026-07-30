(function (root, factory) {
  'use strict';

  const captureYouTubePageState = factory();

  if (typeof module === 'object' && module.exports) {
    module.exports = captureYouTubePageState;
  } else {
    root.__rssPalCaptureYouTubePageState = captureYouTubePageState;
  }
})(globalThis, function () {
  'use strict';

  async function captureYouTubePageState(options, injectedEnvironment) {
    function isObject(value) {
      return value !== null && typeof value === 'object' && !Array.isArray(value);
    }

    function hasOwn(value, key) {
      return Object.prototype.hasOwnProperty.call(value, key);
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

    function copyRange(value) {
      if (!isObject(value)) {
        return null;
      }

      const range = {};
      for (const key of ['start', 'end']) {
        if (hasOwn(value, key)) {
          const field = value[key];
          if (
            field === null ||
            (typeof field !== 'object' && typeof field !== 'function')
          ) {
            range[key] = field;
          }
        }
      }
      return range;
    }

    function sanitizeFormat(raw) {
      if (!isObject(raw)) {
        return null;
      }

      const sanitized = {};
      const scalarKeys = [
        'itag',
        'mimeType',
        'bitrate',
        'width',
        'height',
        'fps',
        'approxDurationMs',
        'audioQuality',
        'audioSampleRate',
        'audioChannels',
      ];

      try {
        for (const key of scalarKeys) {
          if (!hasOwn(raw, key)) {
            continue;
          }

          const value = raw[key];
          if (
            value === null ||
            (typeof value !== 'object' && typeof value !== 'function')
          ) {
            sanitized[key] = value;
          }
        }

        for (const key of ['initRange', 'indexRange']) {
          if (hasOwn(raw, key)) {
            const range = copyRange(raw[key]);
            if (range !== null) {
              sanitized[key] = range;
            }
          }
        }

        if (hasOwn(raw, 'url') && typeof raw.url === 'string') {
          sanitized.url = raw.url;
        }
      } catch {
        return null;
      }

      return sanitized;
    }

    function sanitizeFormats(response) {
      let streamingData;
      try {
        streamingData = isObject(response.streamingData)
          ? response.streamingData
          : {};
      } catch {
        return [];
      }

      const collections = [];
      try {
        collections.push(
          Array.isArray(streamingData.adaptiveFormats)
            ? streamingData.adaptiveFormats
            : [],
          Array.isArray(streamingData.formats) ? streamingData.formats : [],
        );
      } catch {
        return [];
      }

      const formats = [];
      const seenItags = new Set();
      const formatByItag = new Map();

      for (const collection of collections) {
        for (const raw of collection) {
          if (formats.length >= 256) {
            return formats;
          }

          const sanitized = sanitizeFormat(raw);
          if (sanitized === null) {
            continue;
          }

          const itag = parsePositiveInteger(sanitized.itag);
          if (itag !== null && seenItags.has(itag)) {
            const existing = formatByItag.get(itag);
            if (
              existing !== undefined &&
              typeof existing.url !== 'string' &&
              typeof sanitized.url === 'string'
            ) {
              existing.url = sanitized.url;
            }
            continue;
          }

          formats.push(sanitized);
          if (itag !== null) {
            seenItags.add(itag);
            formatByItag.set(itag, sanitized);
          }
        }
      }

      return formats;
    }

    function parseResource(value) {
      if (typeof value !== 'string') {
        return null;
      }

      let parsed;
      try {
        parsed = new URL(value);
      } catch {
        return null;
      }

      const hostname = parsed.hostname.toLowerCase();
      if (
        parsed.protocol !== 'https:' ||
        (hostname !== 'googlevideo.com' &&
          !hostname.endsWith('.googlevideo.com')) ||
        !/^\/videoplayback(?:\/|$)/.test(parsed.pathname)
      ) {
        return null;
      }

      let itag = parsePositiveInteger(parsed.searchParams.get('itag'));
      if (itag === null) {
        const match = parsed.pathname.match(/\/itag\/([^/]+)(?:\/|$)/);
        itag = match === null ? null : parsePositiveInteger(match[1]);
      }

      return itag === null ? null : { url: value, itag };
    }

    function formatCoverageIsReady(formats, resourceItags) {
      let coveredAdaptiveVideo = false;
      let coveredAdaptiveAudio = false;
      let coveredProgressiveVideo = false;

      for (const format of formats) {
        const itag = parsePositiveInteger(format.itag);
        if (itag === null || typeof format.mimeType !== 'string') {
          continue;
        }

        const hasDirectUrl =
          typeof format.url === 'string' && format.url.length > 0;
        if (!hasDirectUrl && !resourceItags.has(itag)) {
          continue;
        }

        const mimeType = format.mimeType.trim().toLowerCase();
        if (mimeType.startsWith('audio/')) {
          coveredAdaptiveAudio = true;
        } else if (mimeType.startsWith('video/')) {
          if (format.audioQuality) {
            coveredProgressiveVideo = true;
          } else {
            coveredAdaptiveVideo = true;
          }
        }
      }

      return (
        coveredProgressiveVideo ||
        (coveredAdaptiveVideo && coveredAdaptiveAudio)
      );
    }

    const environment =
      injectedEnvironment !== null &&
      typeof injectedEnvironment === 'object'
        ? injectedEnvironment
        : {};
    const pageDocument =
      environment.document === undefined
        ? globalThis.document
        : environment.document;
    const pagePerformance =
      environment.performance === undefined
        ? globalThis.performance
        : environment.performance;
    const pageRoot =
      environment.root === undefined ? globalThis.window : environment.root;
    const now =
      typeof environment.now === 'function'
        ? environment.now
        : () => Date.now();
    const sleep =
      typeof environment.sleep === 'function'
        ? environment.sleep
        : (delayMs) =>
            new Promise((resolve) => {
              setTimeout(resolve, delayMs);
            });

    let timeoutMs = 15_000;
    try {
      if (
        options !== null &&
        typeof options === 'object' &&
        typeof options.timeoutMs === 'number' &&
        Number.isFinite(options.timeoutMs)
      ) {
        timeoutMs = Math.min(20_000, Math.max(1000, options.timeoutMs));
      }
    } catch {
      timeoutMs = 15_000;
    }

    const timeoutResult = {
      status: 'CAPTURE_TIMEOUT',
      formats: [],
      resourceUrls: [],
    };
    const deadline = now() + timeoutMs;

    async function sleepUntilNextPoll() {
      const remainingMs = deadline - now();
      if (remainingMs <= 0) {
        return false;
      }
      await sleep(Math.min(250, remainingMs));
      return true;
    }

    let player = null;
    try {
      while (now() < deadline) {
        let candidate = null;
        try {
          candidate = pageDocument.getElementById('movie_player');
          if (
            candidate !== null &&
            typeof candidate.getPlayerResponse === 'function'
          ) {
            player = candidate;
            break;
          }
        } catch {
          candidate = null;
        }

        if (!(await sleepUntilNextPoll())) {
          break;
        }
      }
    } catch {
      return timeoutResult;
    }

    if (player === null) {
      return timeoutResult;
    }

    let response = null;
    try {
      response = player.getPlayerResponse();
    } catch {
      response = null;
    }
    if (!isObject(response)) {
      try {
        response = isObject(pageRoot.ytInitialPlayerResponse)
          ? pageRoot.ytInitialPlayerResponse
          : null;
      } catch {
        response = null;
      }
    }

    let playabilityStatus = 'UNPLAYABLE';
    try {
      const status = response?.playabilityStatus?.status;
      if (typeof status === 'string' && status.length > 0) {
        playabilityStatus = status;
      }
    } catch {
      playabilityStatus = 'UNPLAYABLE';
    }
    if (playabilityStatus !== 'OK') {
      return {
        status: playabilityStatus,
        formats: [],
        resourceUrls: [],
      };
    }

    const formats = sanitizeFormats(response);

    let levels = [];
    try {
      if (typeof player.getAvailableQualityLevels === 'function') {
        const available = player.getAvailableQualityLevels();
        levels = Array.isArray(available) ? available : [];
      }
    } catch {
      levels = [];
    }

    const targetQuality = levels.includes('hd1080')
      ? 'hd1080'
      : levels.includes('hd720')
        ? 'hd720'
        : null;
    if (targetQuality !== null) {
      try {
        if (typeof player.setPlaybackQualityRange === 'function') {
          player.setPlaybackQualityRange(targetQuality, targetQuality);
        } else if (typeof player.setPlaybackQuality === 'function') {
          player.setPlaybackQuality(targetQuality);
        }
      } catch {
        // Quality selection is best-effort.
      }
    }

    try {
      if (typeof player.mute === 'function') {
        player.mute();
      }
    } catch {
      // Muting is best-effort.
    }

    let playbackAttempted = false;
    try {
      if (typeof player.playVideo === 'function') {
        playbackAttempted = true;
        await player.playVideo();
      }

      const resourceUrls = [];
      const seenResourceUrls = new Set();
      const resourceItags = new Set();

      while (true) {
        const entries = pagePerformance.getEntriesByType('resource');
        if (Array.isArray(entries)) {
          for (const entry of entries) {
            if (resourceUrls.length >= 256) {
              break;
            }

            const resource = parseResource(entry?.name);
            if (
              resource === null ||
              seenResourceUrls.has(resource.url)
            ) {
              continue;
            }

            seenResourceUrls.add(resource.url);
            resourceUrls.push(resource.url);
            resourceItags.add(resource.itag);
          }
        }

        if (
          formatCoverageIsReady(formats, resourceItags) ||
          now() >= deadline
        ) {
          break;
        }

        if (!(await sleepUntilNextPoll())) {
          break;
        }
      }

      return {
        status: 'OK',
        formats,
        resourceUrls,
      };
    } catch {
      return timeoutResult;
    } finally {
      if (
        playbackAttempted &&
        typeof player.pauseVideo === 'function'
      ) {
        try {
          await player.pauseVideo();
        } catch {
          // Pausing is best-effort after every playback attempt.
        }
      }
    }
  }

  return captureYouTubePageState;
});
