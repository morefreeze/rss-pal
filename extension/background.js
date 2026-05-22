// Keepalive: chrome.alarms wakes the SW every 30s so Chrome keeps the
// extension's scaffolding warm and the toolbar popup opens faster.
const KEEPALIVE_ALARM = 'rss-pal-keepalive';

function scheduleKeepalive() {
  chrome.alarms.create(KEEPALIVE_ALARM, { periodInMinutes: 0.5 });
}

chrome.runtime.onInstalled.addListener(scheduleKeepalive);
chrome.runtime.onStartup.addListener(scheduleKeepalive);

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === KEEPALIVE_ALARM) {
    chrome.storage.local.get('__keepalive', () => void chrome.runtime.lastError);
  }
});

scheduleKeepalive();
