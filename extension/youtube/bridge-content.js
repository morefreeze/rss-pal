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

  function createYouTubeContentBridge(page, runtime, protocol) {
    const version = runtime.getManifest().version;

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
          page.postMessage(
            {
              ...response,
              type: protocol.RSS_PAL_YOUTUBE_RESOLVE_RESPONSE,
              requestId: message.requestId,
            },
            protocol.RSS_ORIGIN,
          );
        } catch {
          page.postMessage(
            {
              ok: false,
              code: 'INTERNAL_ERROR',
              type: protocol.RSS_PAL_YOUTUBE_RESOLVE_RESPONSE,
              requestId: message.requestId,
            },
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
      page.removeEventListener('message', onMessage);
    };
  }

  return { createYouTubeContentBridge };
});
