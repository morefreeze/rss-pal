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

    function copyNumericValue(value) {
      if (typeof value === 'number') {
        return Number.isFinite(value) ? value : null;
      }

      return typeof value === 'string' && /^\d{1,32}$/.test(value)
        ? value
        : null;
    }

    function copyRange(value) {
      if (!isObject(value)) {
        return null;
      }

      const range = {};
      for (const key of ['start', 'end']) {
        if (hasOwn(value, key)) {
          const field = copyNumericValue(value[key]);
          if (field !== null) {
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
        'bitrate',
        'width',
        'height',
        'fps',
        'approxDurationMs',
        'audioSampleRate',
        'audioChannels',
      ];

      try {
        for (const key of scalarKeys) {
          if (!hasOwn(raw, key)) {
            continue;
          }

          const value = copyNumericValue(raw[key]);
          if (value !== null) {
            sanitized[key] = value;
          }
        }

        for (const key of ['mimeType', 'audioQuality']) {
          if (
            hasOwn(raw, key) &&
            typeof raw[key] === 'string' &&
            raw[key].length <= 512
          ) {
            sanitized[key] = raw[key];
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

        if (
          hasOwn(raw, 'url') &&
          typeof raw.url === 'string' &&
          raw.url.length <= 16_384
        ) {
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
      if (typeof value !== 'string' || value.length > 16_384) {
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

    function hasMimeKind(format, kind) {
      return (
        typeof format.mimeType === 'string' &&
        format.mimeType.trim().toLowerCase().startsWith(`${kind}/`)
      );
    }

    function hasPlaybackMetadata(format) {
      return (
        parsePositiveInteger(format.bitrate) !== null &&
        parsePositiveInteger(format.approxDurationMs) !== null
      );
    }

    function hasAdaptiveRanges(format) {
      return isObject(format.initRange) && isObject(format.indexRange);
    }

    function isEligibleAdaptiveVideo(format) {
      const height = parsePositiveInteger(format.height);
      return (
        hasMimeKind(format, 'video') &&
        !hasOwn(format, 'audioQuality') &&
        height !== null &&
        height >= 720 &&
        height <= 1080 &&
        hasPlaybackMetadata(format) &&
        hasAdaptiveRanges(format)
      );
    }

    function isEligibleAdaptiveAudio(format) {
      return (
        hasMimeKind(format, 'audio') &&
        hasPlaybackMetadata(format) &&
        hasAdaptiveRanges(format)
      );
    }

    function isEligibleProgressive(format) {
      return (
        hasMimeKind(format, 'video') &&
        hasOwn(format, 'audioQuality') &&
        hasPlaybackMetadata(format)
      );
    }

    function buildPlaybackModel(formats) {
      const adaptiveVideos = [];
      const adaptiveAudios = [];
      const progressives = [];

      for (const format of formats) {
        if (isEligibleAdaptiveVideo(format)) {
          adaptiveVideos.push(format);
        } else if (isEligibleAdaptiveAudio(format)) {
          adaptiveAudios.push(format);
        } else if (isEligibleProgressive(format)) {
          progressives.push(format);
        }
      }

      let desiredHeight = null;
      for (const video of adaptiveVideos) {
        const height = parsePositiveInteger(video.height);
        if (
          height !== null &&
          (desiredHeight === null || height > desiredHeight)
        ) {
          desiredHeight = height;
        }
      }

      return {
        adaptiveVideos,
        adaptiveAudios,
        progressives,
        desiredHeight,
        desiredQuality:
          desiredHeight === null
            ? null
            : desiredHeight >= 1080
              ? 'hd1080'
              : 'hd720',
        hasAdaptivePair:
          adaptiveVideos.length > 0 && adaptiveAudios.length > 0,
      };
    }

    function formatIsCovered(format, resourcesByItag) {
      const itag = parsePositiveInteger(format.itag);
      return (
        (typeof format.url === 'string' && format.url.length > 0) ||
        (itag !== null && resourcesByItag.has(itag))
      );
    }

    function anyCovered(formats, resourcesByItag) {
      return formats.some((format) =>
        formatIsCovered(format, resourcesByItag),
      );
    }

    function preferredCoverageIsReady(model, resourcesByItag) {
      if (!model.hasAdaptivePair) {
        return anyCovered(model.progressives, resourcesByItag);
      }

      const preferredVideos = model.adaptiveVideos.filter(
        (format) =>
          parsePositiveInteger(format.height) === model.desiredHeight,
      );
      return (
        anyCovered(preferredVideos, resourcesByItag) &&
        anyCovered(model.adaptiveAudios, resourcesByItag)
      );
    }

    function fallbackCoverageIsReady(model, resourcesByItag) {
      return (
        (anyCovered(model.adaptiveVideos, resourcesByItag) &&
          anyCovered(model.adaptiveAudios, resourcesByItag)) ||
        anyCovered(model.progressives, resourcesByItag)
      );
    }

    function getPlayabilityStatus(response) {
      try {
        const status = response?.playabilityStatus?.status;
        if (
          typeof status !== 'string' ||
          status.trim().length === 0 ||
          status.length > 128
        ) {
          return null;
        }
        return status;
      } catch {
        return null;
      }
    }

    function hasRawFormats(response) {
      try {
        const streamingData = response?.streamingData;
        return (
          isObject(streamingData) &&
          ((Array.isArray(streamingData.adaptiveFormats) &&
            streamingData.adaptiveFormats.length > 0) ||
            (Array.isArray(streamingData.formats) &&
              streamingData.formats.length > 0))
        );
      } catch {
        return false;
      }
    }

    function isUsableResponse(response) {
      if (!isObject(response)) {
        return false;
      }

      const status = getPlayabilityStatus(response);
      return status !== null && (status !== 'OK' || hasRawFormats(response));
    }

    function readUsableResponse(player, pageRoot) {
      let response = null;
      try {
        response = player.getPlayerResponse();
      } catch {
        response = null;
      }
      if (isUsableResponse(response)) {
        return response;
      }

      try {
        response = pageRoot.ytInitialPlayerResponse;
      } catch {
        response = null;
      }
      return isUsableResponse(response) ? response : null;
    }

    function matchesExpectedVideo(player, response, expectedVideoId) {
      if (expectedVideoId === null) {
        return true;
      }

      let identityConfirmed = false;
      try {
        const responseVideoId = response?.videoDetails?.videoId;
        if (responseVideoId !== undefined) {
          if (responseVideoId !== expectedVideoId) {
            return false;
          }
          identityConfirmed = true;
        }
      } catch {
        return false;
      }

      try {
        if (typeof player.getVideoData === 'function') {
          const videoData = player.getVideoData();
          if (isObject(videoData) && hasOwn(videoData, 'video_id')) {
            if (videoData.video_id !== expectedVideoId) {
              return false;
            }
            identityConfirmed = true;
          }
        }
      } catch {
        return false;
      }

      return identityConfirmed;
    }

    function adIsActive(player) {
      try {
        if (
          player.classList !== undefined &&
          player.classList !== null &&
          typeof player.classList.contains === 'function' &&
          player.classList.contains('ad-showing')
        ) {
          return true;
        }
      } catch {
        // Fall through to any available player ad state.
      }

      try {
        if (typeof player.getAdState !== 'function') {
          return false;
        }
        const state = player.getAdState();
        if (state === true) {
          return true;
        }
        if (typeof state === 'number') {
          return Number.isFinite(state) && state > 0;
        }
        if (typeof state === 'string') {
          return ['1', 'active', 'playing', 'ad-showing'].includes(
            state.trim().toLowerCase(),
          );
        }
      } catch {
        return false;
      }

      return false;
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
    let expectedVideoId = null;
    try {
      if (
        options !== null &&
        typeof options === 'object' &&
        typeof options.timeoutMs === 'number' &&
        Number.isFinite(options.timeoutMs)
      ) {
        timeoutMs = Math.min(20_000, Math.max(1000, options.timeoutMs));
      }
      if (
        options !== null &&
        typeof options === 'object' &&
        typeof options.videoId === 'string' &&
        /^[A-Za-z0-9_-]{11}$/.test(options.videoId)
      ) {
        expectedVideoId = options.videoId;
      }
    } catch {
      timeoutMs = 15_000;
      expectedVideoId = null;
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

    function clearResourceTimings() {
      try {
        if (
          pagePerformance === null ||
          typeof pagePerformance.clearResourceTimings !== 'function'
        ) {
          return false;
        }
        pagePerformance.clearResourceTimings();
        return true;
      } catch {
        return false;
      }
    }

    let player = null;
    let response = null;
    try {
      while (now() < deadline) {
        let candidate = null;
        try {
          candidate = pageDocument.getElementById('movie_player');
          if (
            candidate !== null &&
            typeof candidate.getPlayerResponse === 'function'
          ) {
            const candidateResponse = readUsableResponse(
              candidate,
              pageRoot,
            );
            if (
              candidateResponse !== null &&
              matchesExpectedVideo(
                candidate,
                candidateResponse,
                expectedVideoId,
              ) &&
              !adIsActive(candidate)
            ) {
              player = candidate;
              response = candidateResponse;
              break;
            }
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

    if (player === null || response === null) {
      return timeoutResult;
    }

    const playabilityStatus = getPlayabilityStatus(response);
    if (playabilityStatus !== 'OK') {
      const publicStatuses = [
        'LOGIN_REQUIRED',
        'UNPLAYABLE',
        'ERROR',
        'LIVE_STREAM_OFFLINE',
        'AGE_CHECK_REQUIRED',
        'CONTENT_CHECK_REQUIRED',
      ];
      return {
        status: publicStatuses.includes(playabilityStatus)
          ? playabilityStatus
          : 'UNPLAYABLE',
        formats: [],
        resourceUrls: [],
      };
    }

    const formats = sanitizeFormats(response);
    const playbackModel = buildPlaybackModel(formats);
    const formatItags = new Set();
    for (const format of formats) {
      const itag = parsePositiveInteger(format.itag);
      if (itag !== null && formatItags.size < 256) {
        formatItags.add(itag);
      }
    }

    try {
      if (typeof pagePerformance.setResourceTimingBufferSize === 'function') {
        pagePerformance.setResourceTimingBufferSize(1000);
      }
    } catch {
      // Enlarging the buffer is best-effort.
    }
    let resourceTimingResetFailed = false;
    try {
      resourceTimingResetFailed =
        typeof pagePerformance.clearResourceTimings === 'function' &&
        !clearResourceTimings();
    } catch {
      resourceTimingResetFailed = true;
    }
    if (resourceTimingResetFailed) {
      return timeoutResult;
    }

    try {
      if (typeof player.getAvailableQualityLevels === 'function') {
        player.getAvailableQualityLevels();
      }
    } catch {
      // Available quality levels are advisory only.
    }

    if (playbackModel.desiredQuality !== null) {
      let rangeApplied = false;
      try {
        if (typeof player.setPlaybackQualityRange === 'function') {
          player.setPlaybackQualityRange(
            playbackModel.desiredQuality,
            playbackModel.desiredQuality,
          );
          rangeApplied = true;
        }
      } catch {
        rangeApplied = false;
      }

      if (!rangeApplied) {
        try {
          if (typeof player.setPlaybackQuality === 'function') {
            player.setPlaybackQuality(playbackModel.desiredQuality);
          }
        } catch {
          // Quality selection is best-effort.
        }
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

      const resourcesByItag = new Map();
      const preferenceGraceMs = Math.min(4000, timeoutMs / 2);
      let fallbackGraceDeadline = Math.min(
        deadline,
        now() + preferenceGraceMs,
      );
      let captureWindowWasUnsafe = false;

      while (true) {
        const currentResponse = readUsableResponse(player, pageRoot);
        const targetIdentityConfirmed =
          currentResponse !== null &&
          getPlayabilityStatus(currentResponse) === 'OK' &&
          matchesExpectedVideo(
            player,
            currentResponse,
            expectedVideoId,
          );
        const captureIsSafe =
          targetIdentityConfirmed && !adIsActive(player);

        if (!captureIsSafe) {
          resourcesByItag.clear();
          clearResourceTimings();
          captureWindowWasUnsafe = true;

          if (now() >= deadline || !(await sleepUntilNextPoll())) {
            return timeoutResult;
          }
          continue;
        }

        if (captureWindowWasUnsafe) {
          resourcesByItag.clear();
          if (!clearResourceTimings()) {
            if (now() >= deadline || !(await sleepUntilNextPoll())) {
              return timeoutResult;
            }
            continue;
          }
          fallbackGraceDeadline = Math.min(
            deadline,
            now() + preferenceGraceMs,
          );
          captureWindowWasUnsafe = false;

          if (now() >= deadline || !(await sleepUntilNextPoll())) {
            return timeoutResult;
          }
          continue;
        }

        const entries = pagePerformance.getEntriesByType('resource');
        if (Array.isArray(entries)) {
          for (const entry of entries) {
            const resource = parseResource(entry?.name);
            if (
              resource === null ||
              !formatItags.has(resource.itag)
            ) {
              continue;
            }

            if (
              resourcesByItag.has(resource.itag) ||
              resourcesByItag.size < 256
            ) {
              resourcesByItag.set(resource.itag, resource.url);
            }
          }
        }

        if (
          preferredCoverageIsReady(playbackModel, resourcesByItag)
        ) {
          return {
            status: 'OK',
            formats,
            resourceUrls: [...resourcesByItag.values()],
          };
        }

        if (
          now() >= fallbackGraceDeadline &&
          fallbackCoverageIsReady(playbackModel, resourcesByItag)
        ) {
          return {
            status: 'OK',
            formats,
            resourceUrls: [...resourcesByItag.values()],
          };
        }

        if (now() >= deadline) {
          return timeoutResult;
        }

        if (!(await sleepUntilNextPoll())) {
          return timeoutResult;
        }
      }
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
