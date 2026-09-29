(() => {
  const origins = ['https://rss.morefreeze.top', 'http://localhost:5173', 'http://127.0.0.1:5173'];
  if (window.top !== window || !origins.includes(location.origin)) return;
  window.addEventListener('message', async event => {
    if (event.source !== window || event.origin !== location.origin) return;
    const data = event.data;
    if (!data || data.source !== 'rss-pal-englife-page' || !['PING', 'AUTHORIZE'].includes(data.type) || typeof data.requestId !== 'string') return;
    if (data.type === 'AUTHORIZE' && data.serverOrigin !== location.origin) return;
    let result;
    try { result = await chrome.runtime.sendMessage({ channel: 'englife', type: data.type, token: data.token, serverOrigin: data.serverOrigin }); }
    catch (_) { result = { ok: false }; }
    window.postMessage({ source: 'rss-pal-englife-extension', requestId: data.requestId, ok: result?.ok === true, ...(result?.ok ? {} : { code: 'AUTHORIZATION_FAILED' }) }, location.origin);
  });
})();
