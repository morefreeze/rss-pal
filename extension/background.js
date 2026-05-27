// MV3 service worker.
//
// Classic-script service worker (no "type": "module") so we can use
// importScripts() to share queue.js with the content-script context.
// queue.js attaches its API to globalThis.__rssPalQueue, which is the
// service-worker's global, so it's directly callable below.
importScripts('queue.js');

// Keepalive: chrome.alarms wakes the SW every 30s so Chrome keeps the
// extension's scaffolding warm and the toolbar popup opens faster.
const KEEPALIVE_ALARM = 'rss-pal-keepalive';
const FLUSH_ALARM = 'flushQueue';

function scheduleAlarms() {
  chrome.alarms.create(KEEPALIVE_ALARM, { periodInMinutes: 0.5 });
  chrome.alarms.create(FLUSH_ALARM, { periodInMinutes: 1 });
}

chrome.runtime.onInstalled.addListener(scheduleAlarms);
chrome.runtime.onStartup.addListener(scheduleAlarms);

chrome.alarms.onAlarm.addListener(async (alarm) => {
  if (alarm.name === KEEPALIVE_ALARM) {
    chrome.storage.local.get('__keepalive', () => void chrome.runtime.lastError);
    return;
  }
  if (alarm.name === FLUSH_ALARM) {
    try {
      const cfg = await chrome.storage.sync.get(['serverUrl', 'token']);
      if (cfg.serverUrl && cfg.token && globalThis.__rssPalQueue) {
        await globalThis.__rssPalQueue.flush(cfg);
      }
    } catch (e) {
      console.warn('[rss-pal] periodic flush failed:', e);
    }
  }
});

// Content scripts ask us to flush after pushing new items.
chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg && msg.action === 'flushQueue') {
    (async () => {
      try {
        const cfg = await chrome.storage.sync.get(['serverUrl', 'token']);
        if (!cfg.serverUrl || !cfg.token) {
          sendResponse({ sent: 0, skipped: 'no-config' });
          return;
        }
        if (!globalThis.__rssPalQueue) {
          sendResponse({ sent: 0, skipped: 'no-queue' });
          return;
        }
        const result = await globalThis.__rssPalQueue.flush(cfg);
        sendResponse(result);
      } catch (e) {
        sendResponse({ sent: 0, error: String(e && e.message || e) });
      }
    })();
    return true;  // async response
  }
  if (msg && msg.action === 'startSync') {
    // Run asynchronously in the service worker (persistent across popup
    // lifecycle, unlike popup-context setTimeout). Reply immediately so the
    // popup unblocks and can be closed by the user without dropping the work.
    runSync(msg.payload).catch((e) => console.error('[rss-pal] runSync failed:', e));
    sendResponse({ status: 'started' });
    return false;  // sync response, no need to keep channel open
  }
  if (msg && msg.action === 'updateBadge') {
    // Per-tab live badge showing how many items the active adapter sees on
    // this tab right now. Per-tab badges are scoped by Chrome — they vanish
    // when the user switches tabs, and don't clobber the global 401 red '!'
    // or the post-sync green '✓' (those are global; per-tab overrides only on
    // the matching tab).
    const tabId = sender && sender.tab && sender.tab.id;
    if (tabId != null) {
      const n = (typeof msg.count === 'number') ? msg.count : -1;
      try {
        if (n >= 0) {
          chrome.action.setBadgeText({ text: String(n), tabId });
          chrome.action.setBadgeBackgroundColor({
            color: n > 0 ? '#2563eb' : '#9ca3af',  // blue=found, grey=zero
            tabId,
          });
        } else {
          chrome.action.setBadgeText({ text: '', tabId });
        }
      } catch (_) {}
    }
    sendResponse({ ok: true });
    return false;
  }
});

// --- startSync workflow ---
//
// The popup just delegates: open the target tab, wait for the content script
// to scrape + enqueue, flush queued items for THAT source only, close the tab,
// and stash the result in chrome.storage.local.lastSyncResult so the popup can
// render it on next open (popup may have been closed mid-flight).

async function runSync({ url, source, serverUrl, token }) {
  const startedAt = Date.now();
  let tabId = null;
  const result = {
    source,                       // { source_kind, source_id, source_name } from popup
    status: 'running',
    started_at: startedAt,
    completed_at: null,
    accepted: 0,
    skipped: 0,
    feed_id: null,
    feed_name: null,
    server_url: serverUrl,
    error: null,
  };
  await chrome.storage.local.set({ lastSyncResult: result });

  try {
    const tab = await chrome.tabs.create({ url, active: false });
    tabId = tab.id;

    const completed = await waitForTabComplete(tabId, 30000);
    if (!completed) {
      result.status = 'failed';
      result.error = 'tab load timeout';
    } else {
      // Grace period for MutationObserver + scroll-triggered extracts on
      // lazy-loaded sites (x.com). 8s is empirically enough for visible tweets.
      await sleep(8000);

      // Probe the synced tab for its extract count BEFORE flushing, so even if
      // flush ends up processing 0 items we can tell the user "the adapter saw
      // N items but none made it through" vs "the adapter found 0 items —
      // selectors probably misaligned with current DOM".
      // Use world: 'ISOLATED' to match content-script context (where
      // window.__rssPalAdapters lives).
      let probedCount = null;
      try {
        const probe = await chrome.scripting.executeScript({
          target: { tabId },
          world: 'ISOLATED',
          func: () => {
            const reg = window.__rssPalAdapters;
            if (!reg || typeof reg.findFor !== 'function') return null;
            const a = reg.findFor(location);
            if (!a || typeof a.extract !== 'function') return null;
            try {
              const r = a.extract(document);
              return (r && Array.isArray(r.items)) ? r.items.length : null;
            } catch (_) {
              return null;
            }
          },
        });
        if (probe && probe[0] && typeof probe[0].result === 'number') {
          probedCount = probe[0].result;
        }
      } catch (_e) {
        // probe failure is non-fatal — leave probedCount null
      }
      result.probed_count = probedCount;

      const flushed = await flushQueueAndCapture(serverUrl, token, source);
      result.accepted = flushed.accepted;
      result.skipped = flushed.skipped;
      result.feed_id = flushed.feed_id;
      result.feed_name = flushed.feed_name;
      result.status = 'done';
      // Distinguish empty result so popup can warn the user. The most likely
      // cause is that the extract() returned no items — either selectors don't
      // match the current x.com DOM, or the page didn't finish rendering
      // before the 8s grace.
      if (result.accepted === 0 && result.skipped === 0) {
        result.status = 'empty';
      }
    }
  } catch (e) {
    result.status = 'failed';
    result.error = String(e && e.message || e);
  } finally {
    if (tabId != null) {
      try { await chrome.tabs.remove(tabId); } catch (_) {}
    }
    result.completed_at = Date.now();
    await chrome.storage.local.set({ lastSyncResult: result });
    // Brief badge cue so the user notices completion even with popup closed.
    try {
      if (result.status === 'done') {
        chrome.action.setBadgeText({ text: '✓' });
        chrome.action.setBadgeBackgroundColor({ color: '#16a34a' });
        setTimeout(() => {
          try { chrome.action.setBadgeText({ text: '' }); } catch (_) {}
        }, 5000);
      }
    } catch (_) {}
  }
}

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)); }

function waitForTabComplete(tabId, timeoutMs) {
  return new Promise((resolve) => {
    let done = false;
    const finish = (ok) => {
      if (done) return;
      done = true;
      try { chrome.tabs.onUpdated.removeListener(listener); } catch (_) {}
      clearTimeout(t);
      resolve(ok);
    };
    const t = setTimeout(() => finish(false), timeoutMs);
    function listener(updatedId, info) {
      if (updatedId === tabId && info.status === 'complete') finish(true);
    }
    chrome.tabs.onUpdated.addListener(listener);
    // Race condition guard: tab may already be 'complete' before we attached.
    chrome.tabs.get(tabId).then((t2) => {
      if (t2 && t2.status === 'complete') finish(true);
    }).catch(() => {});
  });
}

// flushQueueAndCapture is a sync-scoped flush: it only POSTs queued batches
// matching targetSource (so other queued sources don't get assigned to this
// sync's feed_id by accident) and captures feed_id / feed_name from the
// server response for the popup deep-link.
async function flushQueueAndCapture(serverUrl, token, targetSource) {
  const q = await loadIngestQueue();
  let totalAccepted = 0;
  let totalSkipped = 0;
  let feedId = null;
  let feedName = null;
  const remaining = [];
  for (const batch of q) {
    const matches = batch.source_kind === targetSource.source_kind &&
                    batch.source_id === targetSource.source_id;
    if (!matches) {
      remaining.push(batch);
      continue;
    }
    try {
      const resp = await fetch(serverUrl.replace(/\/+$/, '') + '/api/extension/ingest', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: 'Bearer ' + token,
        },
        body: JSON.stringify({
          source_kind: batch.source_kind,
          source_id: batch.source_id,
          source_name: batch.source_name,
          items: batch.items,
        }),
      });
      if (!resp.ok) {
        // Leave in queue for the next alarm-driven flush to retry.
        remaining.push(batch);
        continue;
      }
      const body = await resp.json();
      totalAccepted += body.accepted || 0;
      totalSkipped += body.skipped || 0;
      if (body.feed_id) feedId = body.feed_id;
      if (body.feed_name) feedName = body.feed_name;
    } catch (_e) {
      remaining.push(batch);
    }
  }
  await chrome.storage.local.set({ ingestQueue: remaining });
  return { accepted: totalAccepted, skipped: totalSkipped, feed_id: feedId, feed_name: feedName };
}

async function loadIngestQueue() {
  const data = await chrome.storage.local.get(['ingestQueue']);
  return Array.isArray(data.ingestQueue) ? data.ingestQueue : [];
}

scheduleAlarms();
