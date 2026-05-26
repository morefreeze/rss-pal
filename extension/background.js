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
chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
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
});

scheduleAlarms();
