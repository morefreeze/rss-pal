(function (root, factory) {
  'use strict';

  const api = factory();

  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  } else {
    root.__rssPalCreateYouTubeResolver = api.createYouTubeResolver;
  }
})(globalThis, function () {
  'use strict';

  const STORAGE_KEY = 'rssPalYouTubeTabs';
  const INTERNAL_ERROR = Object.freeze({
    ok: false,
    code: 'INTERNAL_ERROR',
  });
  const LOCAL_NETWORK_ERROR = Object.freeze({
    ok: false,
    code: 'LOCAL_NETWORK_ERROR',
  });
  const RESOLVE_TIMEOUT = Object.freeze({
    ok: false,
    code: 'RESOLVE_TIMEOUT',
  });

  function createYouTubeResolver({
    chromeApi,
    protocol,
    selectPlayback,
    capturePageState,
    now = Date.now,
    tabTimeoutMs = 30_000,
    captureTimeoutMs = 15_000,
    maxConcurrent = 2,
  } = {}) {
    const activeTabIds = new Set();
    const entriesByVideoId = new Map();
    const requestsById = new Map();
    let persistence = Promise.resolve();

    function persistActiveTabIds() {
      const write = persistence
        .catch(() => {})
        .then(() =>
          chromeApi.storage.session.set({
            [STORAGE_KEY]: [...activeTabIds],
          }),
        );
      persistence = write.catch(() => {});
      return write;
    }

    function waitForTabComplete(tabId) {
      return new Promise((resolve) => {
        let settled = false;
        let timeoutId;

        function finish(result) {
          if (settled) {
            return;
          }
          settled = true;
          clearTimeout(timeoutId);
          chromeApi.tabs.onUpdated.removeListener(onUpdated);
          chromeApi.tabs.onRemoved.removeListener(onRemoved);
          resolve(result);
        }

        function onUpdated(updatedTabId, changeInfo) {
          if (
            updatedTabId === tabId &&
            changeInfo &&
            changeInfo.status === 'complete'
          ) {
            finish('complete');
          }
        }

        function onRemoved(removedTabId) {
          if (removedTabId === tabId) {
            finish('removed');
          }
        }

        chromeApi.tabs.onUpdated.addListener(onUpdated);
        chromeApi.tabs.onRemoved.addListener(onRemoved);
        timeoutId = setTimeout(() => finish('timeout'), tabTimeoutMs);
        chromeApi.tabs.get(tabId)
          .then((tab) => {
            if (tab && tab.status === 'complete') {
              finish('complete');
            }
          })
          .catch(() => finish('error'));
      });
    }

    async function resolveVideo(entry) {
      let tabId = null;
      try {
        const tab = await chromeApi.tabs.create({
          url: `https://www.youtube.com/watch?v=${entry.videoId}`,
          active: false,
        });
        if (!tab || !Number.isInteger(tab.id)) {
          return LOCAL_NETWORK_ERROR;
        }

        tabId = tab.id;
        entry.tabId = tabId;
        if (entry.cancelled || entry.requestIds.size === 0) {
          return INTERNAL_ERROR;
        }
        activeTabIds.add(tabId);
        await persistActiveTabIds();
        if (entry.cancelled || entry.requestIds.size === 0) {
          return INTERNAL_ERROR;
        }

        const loadResult = await waitForTabComplete(tabId);
        if (loadResult !== 'complete') {
          if (entry.cancelled) {
            return INTERNAL_ERROR;
          }
          return loadResult === 'timeout'
            ? RESOLVE_TIMEOUT
            : LOCAL_NETWORK_ERROR;
        }

        let injected;
        try {
          injected = await chromeApi.scripting.executeScript({
            target: { tabId },
            world: 'MAIN',
            func: capturePageState,
            args: [{
              timeoutMs: captureTimeoutMs,
              videoId: entry.videoId,
            }],
          });
        } catch {
          return LOCAL_NETWORK_ERROR;
        }
        const captured = injected && injected[0] && injected[0].result;
        if (
          captured === null ||
          typeof captured !== 'object' ||
          Array.isArray(captured) ||
          typeof captured.status !== 'string' ||
          captured.status.length === 0 ||
          captured.status.length > 128 ||
          !Array.isArray(captured.formats) ||
          !Array.isArray(captured.resourceUrls)
        ) {
          return INTERNAL_ERROR;
        }
        if (captured.status === 'CAPTURE_TIMEOUT') {
          return RESOLVE_TIMEOUT;
        }
        try {
          return selectPlayback(captured, now());
        } catch {
          return INTERNAL_ERROR;
        }
      } catch {
        return LOCAL_NETWORK_ERROR;
      } finally {
        if (tabId !== null) {
          try {
            await chromeApi.tabs.remove(tabId);
          } catch {}
          activeTabIds.delete(tabId);
          try {
            await persistActiveTabIds();
          } catch {}
        }
        if (entriesByVideoId.get(entry.videoId) === entry) {
          entriesByVideoId.delete(entry.videoId);
        }
        for (const requestId of entry.requestIds) {
          requestsById.delete(requestId);
        }
        entry.requestIds.clear();
      }
    }

    function startOrJoinResolution(request, requesterTabId) {
      if (requestsById.has(request.requestId)) {
        return INTERNAL_ERROR;
      }

      const existing = entriesByVideoId.get(request.videoId);
      if (existing) {
        existing.requestIds.add(request.requestId);
        requestsById.set(request.requestId, {
          videoId: request.videoId,
          requesterTabId,
        });
        return existing.promise;
      }

      if (entriesByVideoId.size >= maxConcurrent) {
        return INTERNAL_ERROR;
      }

      const entry = {
        cancelled: false,
        videoId: request.videoId,
        requestIds: new Set([request.requestId]),
        tabId: null,
      };
      entriesByVideoId.set(request.videoId, entry);
      requestsById.set(request.requestId, {
        videoId: request.videoId,
        requesterTabId,
      });
      entry.promise = resolveVideo(entry);
      return entry.promise;
    }

    async function cancelRequest(requestId) {
      const request = requestsById.get(requestId);
      if (!request) {
        return;
      }

      requestsById.delete(requestId);
      const entry = entriesByVideoId.get(request.videoId);
      if (!entry) {
        return;
      }

      entry.requestIds.delete(requestId);
      if (entry.requestIds.size > 0) {
        return;
      }

      entry.cancelled = true;
      if (entriesByVideoId.get(entry.videoId) === entry) {
        entriesByVideoId.delete(entry.videoId);
      }
      if (Number.isInteger(entry.tabId)) {
        try {
          await chromeApi.tabs.remove(entry.tabId);
        } catch {}
      }
    }

    async function handleCancel(request) {
      await cancelRequest(request.requestId);
      return { ok: true };
    }

    function handleMessage(message, sender) {
      if (!protocol.isTrustedSender(sender)) {
        return INTERNAL_ERROR;
      }
      if (
        message === null ||
        typeof message !== 'object' ||
        Array.isArray(message) ||
        typeof message.action !== 'string'
      ) {
        return INTERNAL_ERROR;
      }
      if (message && message.action === protocol.RUNTIME_RESOLVE) {
        const request = protocol.validateRuntimeResolve(message);
        return request === null
          ? INTERNAL_ERROR
          : startOrJoinResolution(request, sender.tab.id);
      }
      if (message && message.action === protocol.RUNTIME_CANCEL) {
        const request = protocol.validateRuntimeCancel(message);
        return request === null ? INTERNAL_ERROR : handleCancel(request);
      }
      return null;
    }

    function cancelRequestsForTab(tabId) {
      if (!Number.isInteger(tabId)) {
        return undefined;
      }
      const requestIds = [];
      for (const [requestId, request] of requestsById) {
        if (request.requesterTabId === tabId) {
          requestIds.push(requestId);
        }
      }
      return Promise.all(
        requestIds.map((requestId) => cancelRequest(requestId)),
      ).then(() => undefined);
    }

    async function cleanupOrphans() {
      await persistence;

      let storedTabIds = [];
      try {
        const stored = await chromeApi.storage.session.get([STORAGE_KEY]);
        if (stored && Array.isArray(stored[STORAGE_KEY])) {
          storedTabIds = stored[STORAGE_KEY];
        }
      } catch {}

      const orphanTabIds = new Set(
        storedTabIds.filter(
          (tabId) =>
            Number.isInteger(tabId) &&
            tabId > 0 &&
            !activeTabIds.has(tabId),
        ),
      );
      for (const tabId of orphanTabIds) {
        try {
          await chromeApi.tabs.remove(tabId);
        } catch {}
      }

      try {
        await persistActiveTabIds();
      } catch {}
    }

    return Object.freeze({
      handleMessage,
      cancelRequestsForTab,
      cleanupOrphans,
    });
  }

  return Object.freeze({ createYouTubeResolver });
});
