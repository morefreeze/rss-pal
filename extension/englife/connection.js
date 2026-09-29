(function (root) {
  'use strict';
  const ORIGINS = ['https://rss.morefreeze.top', 'http://localhost:5173', 'http://127.0.0.1:5173'];
  const PREFIX = 'englife-pair-';
  function createConnection({ chromeApi: chrome, fetchFn = fetch, now = Date.now, randomID = () => crypto.randomUUID() }) {
    const pending = new Set();
    const failure = () => ({ ok: false, code: 'AUTHORIZATION_FAILED' });
    function pageOrigin(sender) {
      try {
        if (sender.id !== chrome.runtime.id || sender.frameId !== 0 || !sender.tab) return null;
        const origin = new URL(sender.tab.url).origin;
        return ORIGINS.includes(origin) && new URL(sender.url).origin === origin ? origin : null;
      } catch (_) { return null; }
    }
    async function cleanup() {
      const all = await chrome.storage.session.get(null);
      for (const [key, value] of Object.entries(all)) {
        if (key.startsWith(PREFIX) && (!value || value.expires <= now())) await chrome.storage.session.remove(key);
      }
    }
    async function handle(message, sender) {
      try {
        if (message.type === 'PING') return pageOrigin(sender) ? { ok: true } : failure();
        if (message.type === 'AUTHORIZE') {
          const origin = pageOrigin(sender);
          if (!origin || message.serverOrigin !== origin || typeof message.token !== 'string' || !message.token || message.token.length > 4096) return failure();
          await cleanup();
          const id = randomID(); const key = PREFIX + id;
          await chrome.storage.session.set({ [key]: { token: message.token, origin, expires: now() + 600000 } });
          try {
            // Bind the newly created tab before its consent script can ask for context.
            const tab = await chrome.tabs.create({ url: 'about:blank' });
            const item = (await chrome.storage.session.get(key))[key];
            await chrome.storage.session.set({ [key]: { ...item, tabId: tab.id } });
            await chrome.tabs.update(tab.id, { url: chrome.runtime.getURL('englife/consent.html') + '?id=' + encodeURIComponent(id) });
          } catch (_) { await chrome.storage.session.remove(key); return failure(); }
          return { ok: true };
        }
        if (!['CONFIRM', 'CANCEL', 'CONTEXT'].includes(message.type) || typeof message.id !== 'string') return failure();
        const expectedURL = chrome.runtime.getURL('englife/consent.html') + '?id=' + encodeURIComponent(message.id);
        if (sender.id !== chrome.runtime.id || sender.frameId !== 0 || sender.url !== expectedURL || sender.tab?.url !== expectedURL) return failure();
        const key = PREFIX + message.id;
        if (pending.has(key)) return failure();
        pending.add(key);
        try {
          const item = (await chrome.storage.session.get(key))[key];
          if (!item || item.tabId !== sender.tab.id) return failure();
          if (message.type === 'CONTEXT' && item.expires > now()) return { ok: true, origin: item.origin };
          await chrome.storage.session.remove(key);
          if (message.type === 'CANCEL') return { ok: true };
          if (item.expires <= now() || !ORIGINS.includes(item.origin)) return failure();
          if (!await chrome.permissions.contains({ permissions: ['cookies'] })) return failure();
          const cookies = (await chrome.cookies.getAll({ domain: 'englife.space' }))
            .filter(cookie => cookie.domain === 'englife.space' || cookie.domain === '.englife.space')
            .map(({ name, value, domain, path, secure, expirationDate }) => ({ name, value, domain, path, secure, ...(expirationDate === undefined ? {} : { expirationDate }) }));
          if (!cookies.length) return failure();
          const response = await fetchFn(item.origin + '/api/integrations/englife/complete', {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'omit', redirect: 'error',
            body: JSON.stringify({ token: item.token, cookies }), signal: AbortSignal.timeout(45000),
          });
          return response.ok ? { ok: true } : failure();
        } finally { pending.delete(key); }
      } catch (_) { return failure(); }
    }
    return { handle, cleanup };
  }
  root.__rssPalEnglife = { createConnection };
  if (typeof module !== 'undefined') module.exports = { createConnection };
})(globalThis);
