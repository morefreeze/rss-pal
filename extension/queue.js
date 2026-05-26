// extension/queue.js
//
// chrome.storage-backed ingest queue with batching, retry, and login-failure
// detection. content.js pushes items; background.js (or popup) flushes.
//
// Designed to work in BOTH contexts:
//   - content scripts (have window, no chrome.action)
//   - MV3 service worker (no window, has chrome.action; loaded via importScripts)
// Hence we attach to globalThis (works everywhere) and guard chrome.action calls.

(function () {
  'use strict';

  if (globalThis.__rssPalQueue) return;

  const STORAGE_KEY = 'ingestQueue';
  const MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;  // 7 days
  const BATCH_SIZE = 50;

  async function loadQueue() {
    const data = await chrome.storage.local.get([STORAGE_KEY]);
    return Array.isArray(data[STORAGE_KEY]) ? data[STORAGE_KEY] : [];
  }

  async function saveQueue(q) {
    await chrome.storage.local.set({ [STORAGE_KEY]: q });
  }

  // push(batch) — batch is { source_kind, source_id, source_name, items: [] }
  // Dedupe by (source_kind, source_id, item.id) within the queue.
  async function push(batch) {
    if (!batch || !batch.items || !batch.items.length) return;
    const q = await loadQueue();
    const existing = new Set();
    for (const b of q) {
      if (b.source_kind === batch.source_kind && b.source_id === batch.source_id) {
        for (const it of b.items) existing.add(it.id);
      }
    }
    const newItems = batch.items.filter((it) => !existing.has(it.id));
    if (!newItems.length) return;
    q.push({
      source_kind: batch.source_kind,
      source_id: batch.source_id,
      source_name: batch.source_name,
      items: newItems,
      queued_at: Date.now(),
    });
    await saveQueue(q);
  }

  function setLoginFailureBadge() {
    // chrome.action is only available in the service worker context.
    if (typeof chrome !== 'undefined' && chrome.action && chrome.action.setBadgeText) {
      try {
        chrome.action.setBadgeText({ text: '!' });
        if (chrome.action.setBadgeBackgroundColor) {
          chrome.action.setBadgeBackgroundColor({ color: '#dc2626' });
        }
      } catch (_) {
        // ignore — likely running in content-script context
      }
    }
  }

  async function flush({ serverUrl, token }) {
    let q = await loadQueue();
    if (!q.length) return { sent: 0 };

    const now = Date.now();
    q = q.filter((b) => now - b.queued_at < MAX_AGE_MS);
    let sent = 0;
    const remaining = [];
    for (const batch of q) {
      // Chunk batch.items into BATCH_SIZE-sized POSTs
      for (let i = 0; i < batch.items.length; i += BATCH_SIZE) {
        const slice = batch.items.slice(i, i + BATCH_SIZE);
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
              items: slice,
            }),
          });
          if (resp.status === 401) {
            // login expired → leave the rest in queue, surface via badge
            remaining.push({ ...batch, items: batch.items.slice(i) });
            await saveQueue(remaining.concat(q.slice(q.indexOf(batch) + 1)));
            setLoginFailureBadge();
            return { sent, error: 'unauthorized' };
          }
          if (!resp.ok) {
            // 5xx — leave for next flush
            remaining.push({ ...batch, items: batch.items.slice(i) });
            continue;
          }
          sent += slice.length;
        } catch (e) {
          remaining.push({ ...batch, items: batch.items.slice(i) });
        }
      }
    }
    await saveQueue(remaining);
    return { sent };
  }

  globalThis.__rssPalQueue = { push, flush, loadQueue };
})();
