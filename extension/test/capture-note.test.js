const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { webcrypto } = require('node:crypto');
const { JSDOM } = require('jsdom');
const root = path.resolve(__dirname, '..');
const tick = () => new Promise(r => setTimeout(r, 15));
async function popup(opts = {}) {
  const saved = opts.saved || {};
  const requests = [];
  const config = { serverUrl: 'https://rss.example', token: opts.token || 'secret' };
  const tab = { id: 7, url: opts.url || 'https://example.org/article', title: 'Example' };
  const dom = new JSDOM(fs.readFileSync(path.join(root, 'popup.html'), 'utf8'), { url: 'https://extension.test', runScripts: 'outside-only' });
  const w = dom.window;
  Object.defineProperty(w, 'crypto', { value: webcrypto });
  w.TextEncoder = TextEncoder;
  w.chrome = {
    storage: {
      sync: { get: async () => config, set: async () => {} },
      local: { get: async keys => Object.fromEntries(keys.map(k => [k, saved[k]])), set: async values => Object.assign(saved, values), remove: async key => { delete saved[key]; } },
      onChanged: { addListener() {} },
    },
    tabs: { query: async () => [{ ...tab }], sendMessage: async (_, msg) => msg.action === 'captureHtml' ? { html: '<html><body>example content</body></html>' } : (opts.adapter || null) },
    scripting: { executeScript: async () => [{ result: opts.selection || '' }] },
    runtime: { openOptionsPage() {}, sendMessage: async () => ({}) },
  };
  w.fetch = async (url, init) => {
    if (!init) return { ok: true, blob: async () => new w.Blob(['PDF']) };
    requests.push({ url, init });
    if (opts.pending) await opts.pending;
    if (opts.error) throw new Error('网络断开');
    return { ok: true, json: async () => opts.result || { status: 'created', article_id: 9, note_saved: true } };
  };
  const helper = path.join(root, 'capture-note.js');
  if (fs.existsSync(helper)) w.eval(fs.readFileSync(helper, 'utf8'));
  w.eval(fs.readFileSync(path.join(root, 'popup.js'), 'utf8'));
  for (let i = 0; i < 20 && w.document.getElementById('captureBtn').disabled; i++) await tick();
  await tick();
  const input = w.document.getElementById('captureNote');
  const write = value => { assert.ok(input, 'popup offers a free capture-note input'); input.value = value; input.dispatchEvent(new w.Event('input', { bubbles: true })); };
  return { w, dom, saved, input, write, requests, config, tab, click: id => w.document.getElementById(id).click() };
}
test('prompt chips preserve free writing; quote appends selected text and keyboard submits', async () => {
  const p = await popup({ selection: '值得引用的原文' });
  p.write('已有想法');
  p.w.document.querySelector('[data-note-prompt]').click();
  assert.ok(p.input.value.startsWith('已有想法\n'));
  p.click('quoteSelection');
  assert.ok(p.input.value.includes('> 值得引用的原文'));
  p.input.dispatchEvent(new p.w.KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }));
  await tick();
  assert.equal(p.requests.length, 1);
  assert.ok(JSON.parse(p.requests[0].init.body).capture_note.includes('已有想法'));
  assert.equal(p.input.value, '');
  p.dom.window.close();
});
test('draft survives reopening; accounts and URLs do not share drafts', async () => {
  const saved = {};
  const p = await popup({ saved }); p.write('群里看到，想用于重构'); await tick(); p.dom.window.close();
  const restored = await popup({ saved }); assert.equal(restored.input.value, '群里看到，想用于重构'); restored.dom.window.close();
  const otherUser = await popup({ saved, token: 'another-secret' }); assert.equal(otherUser.input.value, ''); otherUser.dom.window.close();
  const otherPage = await popup({ saved, url: 'https://example.org/other' }); assert.equal(otherPage.input.value, ''); otherPage.dom.window.close();
  assert.ok(Object.keys(saved).every(key => !key.includes('secret')));
});
test('network failure and old server keep the note draft', async () => {
  for (const opts of [{ error: true }, { result: { status: 'created', article_id: 9 } }]) {
    const p = await popup(opts); p.write('不能丢的想法'); p.click('captureBtn'); await tick();
    assert.equal(p.input.value, '不能丢的想法');
    assert.ok(Object.values(p.saved).some(v => v === '不能丢的想法'));
    if (!opts.error) assert.match(p.w.document.getElementById('status').textContent, /随手记.*未.*保存|随手记.*未.*确认/);
    p.dom.window.close();
  }
});
test('duplicate saves note; overwrite keeps it exactly once in the submitted request', async () => {
  const p = await popup({ result: { status: 'duplicate', article_id: 9, note_saved: true } });
  p.write('收藏理由'); p.click('captureBtn'); await tick();
  assert.match(p.w.document.getElementById('dupMessage').textContent, /随手记已保存/);
  assert.equal(p.input.value, '');
  p.click('overwriteBtn'); await tick();
  assert.equal(JSON.parse(p.requests[1].init.body).capture_note || '', '');
  p.dom.window.close();
});
test('PDF sends note as multipart field and clears acknowledged draft', async () => {
  const p = await popup({ url: 'https://example.org/paper.pdf' });
  p.write('论文里的方法值得试'); p.click('captureBtn'); await tick();
  assert.ok(p.requests[0].url.endsWith('/capture-pdf'));
  assert.equal(p.requests[0].init.body.get('capture_note'), '论文里的方法值得试');
  assert.equal(p.input.value, ''); p.dom.window.close();
});
test('blank note still allows save; IME enter does not submit', async () => {
  const p = await popup(); p.write('');
  p.input.dispatchEvent(new p.w.KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, isComposing: true, bubbles: true }));
  await tick(); assert.equal(p.requests.length, 0);
  p.click('captureBtn'); await tick(); assert.equal(p.requests.length, 1); p.dom.window.close();
});

test('saving locks all note controls and prevents duplicate keyboard requests', async () => {
  let done;
  const pending = new Promise(resolve => { done = resolve; });
  const p = await popup({ pending }); p.write('当前想法'); p.click('captureBtn'); await tick();
  assert.equal(p.w.document.getElementById('captureNoteFields').disabled, true);
  p.input.dispatchEvent(new p.w.KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }));
  assert.equal(p.requests.length, 1);
  done(); await tick(); assert.equal(p.input.value, ''); p.dom.window.close();
});
test('changing page or account before saving retains draft without submitting it elsewhere', async () => {
  for (const kind of ['page', 'account']) {
    const p = await popup(); p.write('私人草稿');
    if (kind === 'page') p.tab.url = 'https://example.org/changed';
    else p.config.token = 'changed-token';
    p.click('captureBtn'); await tick();
    assert.equal(p.requests.length, 0); assert.equal(p.input.value, '私人草稿');
    p.dom.window.close();
  }
});
test('too-long note is kept for editing and not silently truncated or submitted', async () => {
  const p = await popup(); p.write('字'.repeat(2001)); p.click('captureBtn'); await tick();
  assert.equal(p.requests.length, 0); assert.equal(p.input.value.length, 2001); p.dom.window.close();
});
test('adapter pages explicitly distinguish single-page notes from batch capture', async () => {
  const p = await popup({ adapter: { matched: true, items: [{ id: '1' }], sourceName: 'X' } });
  assert.equal(p.w.document.getElementById('captureNoteScope').hidden, false);
  assert.match(p.w.document.getElementById('captureNoteScope').textContent, /批量抓取不包含/);
  p.dom.window.close();
});
