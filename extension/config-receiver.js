(function () {
  'use strict';
  console.log('[RSS Pal] config-receiver loaded on', location.href);
  if (location.pathname !== '/extension-config') {
    console.log('[RSS Pal] path mismatch, skipping');
    return;
  }
  var raw = location.hash.replace(/^#/, '');
  if (!raw) {
    console.log('[RSS Pal] no hash, skipping');
    return;
  }
  var params = new URLSearchParams(raw);
  var token = params.get('token');
  var serverUrl = params.get('serverUrl');
  if (!token || !serverUrl) {
    console.log('[RSS Pal] missing token or serverUrl in hash');
    return;
  }

  chrome.storage.sync.set({ serverUrl: serverUrl, token: token }, function () {
    console.log('[RSS Pal] config saved, notifying page');
    function notify() {
      window.postMessage({ type: 'RSS_PAL_EXTENSION_CONFIGURED' }, '*');
    }
    notify();
    setTimeout(notify, 200);
    setTimeout(notify, 800);
  });

  // Strip secrets from the address bar after the save call is queued.
  try { history.replaceState(null, '', location.pathname); } catch (e) {}
})();
