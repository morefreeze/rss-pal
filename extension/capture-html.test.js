const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const { webcrypto } = require('node:crypto');
const { JSDOM } = require('jsdom');

const read = name => fs.readFileSync(`${__dirname}/${name}`, 'utf8');
const flush = () => new Promise(resolve => setImmediate(resolve));

async function capture(html, url, mode) {
  const page = new JSDOM(html, { url, runScripts: 'outside-only' });
  const before = page.window.document.documentElement.outerHTML;
  let captured;
  let popup;
  try {
    if (mode === 'content script') {
      const listeners = [];
      page.window.chrome = { runtime: { onMessage: { addListener: listener => listeners.push(listener) } } };
      page.window.eval(read('content.js'));
      listeners[0]({ action: 'captureHtml' }, {}, response => { captured = response.html; });
    } else {
      popup = new JSDOM(read('popup.html'), { runScripts: 'outside-only' });
      Object.defineProperty(popup.window, 'crypto', { value: webcrypto });
      popup.window.TextEncoder = TextEncoder;
      popup.window.chrome = {
        storage: { sync: { get: async () => ({ serverUrl: 'https://rss.test', token: 'test-token' }) }, local: { get: async () => ({}) } },
        tabs: { query: async () => [{ id: 1, url, title: 'Article' }], sendMessage: async () => { throw new Error('No content script'); } },
        scripting: { executeScript: async ({ func }) => [{ result: page.window.eval(`(${func.toString()})()`) }] },
      };
      popup.window.fetch = async (_url, options) => {
        captured = JSON.parse(options.body).html;
        return { ok: true, json: async () => ({ status: 'ok' }) };
      };
      popup.window.eval(read('capture-note.js'));
      popup.window.eval(read('popup.js'));
      for (let i = 0; i < 40 && popup.window.document.getElementById('captureBtn').disabled; i++) {
        await new Promise(resolve => setTimeout(resolve, 5));
      }
      popup.window.document.getElementById('captureBtn').click();
      await flush();
    }
    assert.ok(captured, 'capture returned HTML');
    assert.equal(page.window.document.documentElement.outerHTML, before, 'capture leaves source DOM unchanged');
    return new JSDOM(captured).window.document;
  } finally {
    page.window.close();
    popup?.window.close();
  }
}

for (const mode of ['content script', 'popup fallback']) {
  test(`${mode}: retains publication JSON-LD and article content while removing executable scripts`, async () => {
    const metadata = { '@context': 'https://schema.org', '@type': 'Article', datePublished: '2026-09-20T08:30:00+08:00', dateModified: '2026-10-14T10:00:00+08:00' };
    const doc = await capture(`<html><head><title>Article</title>
      <meta property="article:published_time" content="2026-09-20T08:30:00+08:00">
      <script type="application/ld+json">${JSON.stringify(metadata)}</script>
      <script>window.unwanted = true;</script><script type="module" src="/app.js"></script>
      <style>body{color:red}</style></head><body><article><p>完整正文</p>
      <img data-src="https://example.test/full.jpg" data-srcset="https://example.test/full-2x.jpg 2x">
      <math><mi>x</mi><mo>+</mo><mn>1</mn></math></article></body></html>`, 'https://example.test/article', mode);
    assert.equal(doc.querySelectorAll('script').length, 1);
    assert.deepEqual(JSON.parse(doc.querySelector('script[type="application/ld+json"]').textContent), metadata);
    assert.equal(doc.querySelector('meta[property="article:published_time"]').content, metadata.datePublished);
    assert.equal(doc.querySelector('article p').textContent, '完整正文');
    assert.equal(doc.querySelector('img').getAttribute('src'), 'https://example.test/full.jpg');
    assert.equal(doc.querySelector('img').getAttribute('srcset'), 'https://example.test/full-2x.jpg 2x');
    assert.equal(doc.querySelector('math').textContent, 'x+1');
    assert.equal(doc.querySelector('style'), null);
  });

  for (const publication of [
    { name: 'visible absolute date', head: '', time: '2026年9月20日 08:30', expected: '2026年9月20日 08:30' },
    { name: 'datetime attribute', head: '', time: '<time datetime="2026-09-20T08:30:00+08:00">昨天</time>', expected: '2026-09-20T08:30:00+08:00' },
    { name: 'publication meta before relative display', head: '<meta property="article:published_time" content="2026-09-20T08:30:00+08:00">', time: '昨天', expected: '2026-09-20T08:30:00+08:00' },
    { name: 'relative display without publication evidence', head: '<meta property="article:modified_time" content="2026-10-14T10:00:00+08:00">', time: '昨天', expected: null },
  ]) {
    test(`${mode}: WeChat ${publication.name}`, async () => {
      const time = publication.time.startsWith('<time') ? publication.time.replace('<time ', '<time id="publish_time" ') : `<em id="publish_time">${publication.time}</em>`;
      const doc = await capture(`<html><head>${publication.head}</head><body>
        <h1 id="activity-name">微信文章</h1>${time}<div class="comment_area"><time>2026-10-14</time></div>
        <div id="js_content"><p>完整正文</p><img data-src="https://mmbiz.qpic.cn/full/0"><math><mi>x</mi></math><script>window.unwanted=true</script></div>
        </body></html>`, 'https://mp.weixin.qq.com/s/article', mode);
      assert.equal((doc.querySelector('meta[property="article:published_time"]') || doc.querySelector('meta[name="rsspal:wechat_publish_time"]'))?.content ?? null, publication.expected);
      if (!publication.head.includes('article:published_time')) assert.equal(doc.querySelector('meta[property="article:published_time"]'), null);
      assert.equal(doc.querySelector('#js_content p').textContent, '完整正文');
      assert.equal(doc.querySelector('img').getAttribute('src'), 'https://mmbiz.qpic.cn/full/0');
      assert.equal(doc.querySelector('math').textContent, 'x');
      assert.equal(doc.querySelector('script'), null);
    });
  }

  for (const attributes of [
    'property="article:published_time"',
    'name="datePublished"',
    'name="publish_date"',
    'name="publication_date"',
  ]) {
    test(`${mode}: WeChat preserves head ${attributes} without a browser date parser`, async () => {
      const doc = await capture(`<html><head><meta ${attributes} content="Sun, 20 Sep 2026 08:30:00 +0800"></head>
        <body><em id="publish_time">2026年9月19日 08:30</em><div id="js_content"><p>正文</p></div></body></html>`, 'https://mp.weixin.qq.com/s/article', mode);
      assert.equal(doc.querySelector(`head meta[${attributes}]`)?.content, 'Sun, 20 Sep 2026 08:30:00 +0800');
      assert.equal(doc.querySelector('head meta[name="rsspal:wechat_publish_time"]')?.content, '2026年9月19日 08:30', 'visible date remains below every original metadata tier');
    });
  }

  for (const area of ['class="comment_area"', 'id="discussion-comments"', 'class="related-articles"', 'id="related-articles"', 'id="recommend-list"', 'itemscope itemtype="https://schema.org/Comment"']) {
    test(`${mode}: WeChat does not promote metadata or publish_time from ${area}`, async () => {
      const doc = await capture(`<html><head></head><body><div id="js_content"><p>正文</p>
        <div ${area}><meta itemprop="datePublished" content="2026-10-14T10:00:00+08:00"><em id="publish_time">2026年10月14日 10:00</em></div>
        </div></body></html>`, 'https://mp.weixin.qq.com/s/article', mode);
      assert.equal(doc.querySelector('head meta[property="article:published_time"]'), null);
      assert.equal(doc.querySelector('head meta[name="rsspal:wechat_publish_time"]'), null);
    });
  }

  test(`${mode}: WeChat keeps Article JSON-LD above a different visible publication date`, async () => {
    const metadata = { '@type': 'Article', datePublished: '2026-09-20T08:30:00+08:00' };
    const doc = await capture(`<html><head><script type="application/ld+json">${JSON.stringify(metadata)}</script></head>
      <body><em id="publish_time">2026年9月19日 08:30</em><div id="js_content"><p>正文</p></div></body></html>`, 'https://mp.weixin.qq.com/s/article', mode);
    assert.deepEqual(JSON.parse(doc.querySelector('script[type="application/ld+json"]')?.textContent || 'null'), metadata);
    assert.equal(doc.querySelector('meta[name="rsspal:wechat_publish_time"]')?.content, '2026年9月19日 08:30');
    assert.equal(doc.querySelector('meta[property="article:published_time"]'), null);
  });

  test(`${mode}: WeChat does not move comment JSON-LD into article metadata`, async () => {
    const doc = await capture(`<html><head></head><body><div id="js_content"><p>正文</p>
      <div class="comment_area"><script type="application/ld+json">{"@type":"Article","datePublished":"2026-10-14"}</script></div>
      </div></body></html>`, 'https://mp.weixin.qq.com/s/article', mode);
    assert.equal(doc.querySelector('head script[type="application/ld+json"]'), null);
  });
}
