(function (root, factory) {
  'use strict';

  const protocol = factory();

  if (typeof module === 'object' && module.exports) {
    module.exports = protocol;
  } else {
    root.__rssPalYouTubeProtocol = protocol;
  }
})(globalThis, function () {
  'use strict';

  const RSS_ORIGIN = 'https://rss.morefreeze.top';

  const RSS_PAL_YOUTUBE_BRIDGE_PING = 'RSS_PAL_YOUTUBE_BRIDGE_PING';
  const RSS_PAL_YOUTUBE_BRIDGE_READY = 'RSS_PAL_YOUTUBE_BRIDGE_READY';
  const RSS_PAL_YOUTUBE_RESOLVE_REQUEST = 'RSS_PAL_YOUTUBE_RESOLVE_REQUEST';
  const RSS_PAL_YOUTUBE_RESOLVE_CANCEL = 'RSS_PAL_YOUTUBE_RESOLVE_CANCEL';
  const RSS_PAL_YOUTUBE_RESOLVE_RESPONSE = 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE';

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

  function hasExactKeys(value, expectedKeys) {
    if (value === null || typeof value !== 'object' || Array.isArray(value)) {
      return false;
    }

    const actualKeys = Reflect.ownKeys(value);
    return (
      actualKeys.length === expectedKeys.length &&
      expectedKeys.every((key) => actualKeys.includes(key))
    );
  }

  function validateRuntimeResolve(message) {
    if (
      !hasExactKeys(message, ['action', 'requestId', 'videoId']) ||
      message.action !== RUNTIME_RESOLVE ||
      !isRequestId(message.requestId) ||
      !isVideoId(message.videoId)
    ) {
      return null;
    }

    return {
      requestId: message.requestId,
      videoId: message.videoId,
    };
  }

  function validateRuntimeCancel(message) {
    if (
      !hasExactKeys(message, ['action', 'requestId']) ||
      message.action !== RUNTIME_CANCEL ||
      !isRequestId(message.requestId)
    ) {
      return null;
    }

    return {
      requestId: message.requestId,
    };
  }

  function isTrustedSender(sender) {
    if (
      sender === null ||
      typeof sender !== 'object' ||
      sender.tab === null ||
      typeof sender.tab !== 'object' ||
      !Number.isInteger(sender.tab.id) ||
      typeof sender.tab.url !== 'string'
    ) {
      return false;
    }

    try {
      return new URL(sender.tab.url).origin === RSS_ORIGIN;
    } catch {
      return false;
    }
  }

  return {
    RSS_ORIGIN,
    RSS_PAL_YOUTUBE_BRIDGE_PING,
    RSS_PAL_YOUTUBE_BRIDGE_READY,
    RSS_PAL_YOUTUBE_RESOLVE_REQUEST,
    RSS_PAL_YOUTUBE_RESOLVE_CANCEL,
    RSS_PAL_YOUTUBE_RESOLVE_RESPONSE,
    RUNTIME_RESOLVE,
    RUNTIME_CANCEL,
    VIDEO_ID_RE,
    REQUEST_ID_RE,
    isVideoId,
    isRequestId,
    validateRuntimeResolve,
    validateRuntimeCancel,
    isTrustedSender,
  };
});
