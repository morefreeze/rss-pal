(function () {
  'use strict';
  if (location.pathname !== '/extension-config') return;
  var raw = location.hash.replace(/^#/, '');
  if (!raw) return;
  var params = new URLSearchParams(raw);
  var token = params.get('token');
  var serverUrl = params.get('serverUrl');
  if (!token || !serverUrl) return;

  chrome.storage.sync.set({ serverUrl: serverUrl, token: token }, function () {
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
