(function (root, factory) {
  'use strict';

  const api = factory();

  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  } else {
    api.createYouTubeContentBridge(
      root.window,
      root.chrome.runtime,
      root.__rssPalYouTubeProtocol,
    );
  }
})(globalThis, function () {
  'use strict';

  const RESOLVE_ERROR_CODES = [
    'LOGIN_REQUIRED',
    'VIDEO_UNAVAILABLE',
    'NO_SUPPORTED_FORMAT',
    'RESOLVE_TIMEOUT',
    'LOCAL_NETWORK_ERROR',
    'PLAYBACK_EXPIRED',
    'INTERNAL_ERROR',
  ];

  function isObject(value) {
    return value !== null && typeof value === 'object' && !Array.isArray(value);
  }

  function normalizeResponse(response, requestId, protocol) {
    const envelope = {
      type: protocol.RSS_PAL_YOUTUBE_RESOLVE_RESPONSE,
      requestId,
    };

    if (
      isObject(response) &&
      response.ok === true &&
      isObject(response.playback)
    ) {
      return {
        ...envelope,
        ok: true,
        playback: response.playback,
      };
    }

    if (
      isObject(response) &&
      response.ok === false &&
      RESOLVE_ERROR_CODES.includes(response.code)
    ) {
      return {
        ...envelope,
        ok: false,
        code: response.code,
      };
    }

    return {
      ...envelope,
      ok: false,
      code: 'INTERNAL_ERROR',
    };
  }

  function createYouTubeContentBridge(page, runtime, protocol) {
    const version = runtime.getManifest().version;
    let active = true;

    function postReady() {
      page.postMessage(
        {
          type: protocol.RSS_PAL_YOUTUBE_BRIDGE_READY,
          version,
        },
        protocol.RSS_ORIGIN,
      );
    }

    async function onMessage(event) {
      if (
        !active ||
        event.source !== page ||
        event.origin !== protocol.RSS_ORIGIN ||
        event.data === null ||
        typeof event.data !== 'object'
      ) {
        return;
      }

      const message = event.data;

      if (message.type === protocol.RSS_PAL_YOUTUBE_BRIDGE_PING) {
        postReady();
        return;
      }

      if (message.type === protocol.RSS_PAL_YOUTUBE_RESOLVE_REQUEST) {
        if (
          !protocol.isRequestId(message.requestId) ||
          !protocol.isVideoId(message.videoId)
        ) {
          return;
        }

        try {
          const response = await runtime.sendMessage({
            action: protocol.RUNTIME_RESOLVE,
            requestId: message.requestId,
            videoId: message.videoId,
          });
          if (!active) {
            return;
          }
          page.postMessage(
            normalizeResponse(response, message.requestId, protocol),
            protocol.RSS_ORIGIN,
          );
        } catch {
          if (!active) {
            return;
          }
          page.postMessage(
            normalizeResponse(undefined, message.requestId, protocol),
            protocol.RSS_ORIGIN,
          );
        }
        return;
      }

      if (
        message.type === protocol.RSS_PAL_YOUTUBE_RESOLVE_CANCEL &&
        protocol.isRequestId(message.requestId)
      ) {
        try {
          await runtime.sendMessage({
            action: protocol.RUNTIME_CANCEL,
            requestId: message.requestId,
          });
        } catch {
          // Cancellation is best-effort.
        }
      }
    }

    page.addEventListener('message', onMessage);
    postReady();

    return function destroy() {
      active = false;
      page.removeEventListener('message', onMessage);
    };
  }

  return { createYouTubeContentBridge };
});
