const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');
const fs = require('node:fs');
function page() {
    const dom = new JSDOM('<html><body><article>Reddit loaded post</article></body></html>', { url: 'https://www.reddit.com/r/test/comments/abc123/title/', runScripts: 'outside-only' });
    const w = dom.window;
    let now = 0, focus = true, tick, listener;
    const messages = [];
    Object.defineProperty(w.performance, 'now', { value: () => now });
    w.document.hasFocus = () => focus;
    Object.defineProperty(w.document, 'visibilityState', { value: 'visible', configurable: true });
    w.setInterval = fn => { tick = fn; };
    w.chrome = { runtime: { sendMessage: async (m) => { messages.push(m); return m.action === 'redditReminderTick' ? { ok: true, show: true } : m.action === 'redditReminderDisable' ? { ok: true, disabled: true } : { ok: true, articleURL: 'https://rss.example/articles/42' }; } }, storage: { onChanged: { addListener: fn => listener = fn } } };
    w.eval(fs.readFileSync(__dirname + '/reddit-reminder.js', 'utf8'));
    w.eval(fs.readFileSync(__dirname + '/reddit-reminder-content.js', 'utf8'));
    return { w, messages, step: async (active = true) => { focus = active; now += 1000; await tick(); }, disable: () => listener({ redditRemindersDisabled: { newValue: true } }, 'sync') };
}
test('foreground ticks stop in background and global closure removes reminder', async () => { const p = page(); for (let i = 0; i < 10; i++)
    await p.step(false); assert.equal(p.messages.length, 0); for (let i = 0; i < 6; i++)
    await p.step(); assert.equal(p.messages.length, 1); assert.equal(p.messages[0].elapsedMs, 5000); assert.ok(p.w.document.getElementById('rss-pal-reddit-reminder')); p.disable(); assert.equal(p.w.document.getElementById('rss-pal-reddit-reminder'), null); for (let i = 0; i < 10; i++)
    await p.step(); assert.equal(p.messages.length, 1); p.w.close(); });
test('SPA navigation removes stale reminder and resets unsent time', async () => { const p = page(); for (let i = 0; i < 6; i++)
    await p.step(); assert.ok(p.w.document.getElementById('rss-pal-reddit-reminder')); p.w.history.pushState({}, '', '/r/test/'); await p.step(); assert.equal(p.w.document.getElementById('rss-pal-reddit-reminder'), null); for (let i = 0; i < 6; i++)
    await p.step(); assert.equal(p.messages.length, 1); p.w.close(); });
