#!/usr/bin/env node
// Smoke test for twitter adapters using a synthetic minimal Twitter tweet element.
// Run: node extension/adapters/twitter/smoke-test.js
//
// This verifies the extract() functions don't throw and return the expected shape.
// It does NOT verify they extract real Twitter content correctly — that's the
// Phase J manual e2e job.

const { JSDOM } = require('jsdom');
const path = require('path');

// Minimal Twitter-like article element. Real Twitter HTML is much richer,
// but extract() should at minimum return { items, sourceID, sourceName, hasMore }
// without crashing.
const MINIMAL_HTML = `
<!DOCTYPE html><html><body>
<h2><span dir="ltr">My Test List</span></h2>
<div data-testid="UserName"><span>Test User</span><span>@testuser</span></div>
<main>
  <article data-testid="tweet" tabindex="0">
    <div data-testid="User-Name">
      <span>Test User</span>
      <span>@testuser</span>
    </div>
    <a href="/testuser/status/1234567890" role="link">
      <time datetime="2026-05-26T10:00:00.000Z">2h</time>
    </a>
    <div data-testid="tweetText" lang="en"><span>Hello world tweet body</span></div>
    <div data-testid="reply" aria-label="3 Replies"><span>3</span></div>
    <div data-testid="retweet" aria-label="5 Retweets"><span>5</span></div>
    <div data-testid="like" aria-label="42 Likes"><span>42</span></div>
  </article>
</main>
</body></html>
`;

const tests = [
  { name: 'list-tweets', path: '/i/lists/9999', file: './list-tweets.js' },
  { name: 'tweets',      path: '/testuser',     file: './tweets.js' },
  { name: 'bookmarks',   path: '/i/bookmarks',  file: './bookmarks.js' },
];

let failed = 0;
for (const t of tests) {
  const dom = new JSDOM(MINIMAL_HTML, { url: 'https://x.com' + t.path });
  globalThis.window = dom.window;
  globalThis.document = dom.window.document;
  globalThis.location = dom.window.location;
  let capturedAdapter;
  globalThis.__rssPalAdapters = {
    register(a) { capturedAdapter = a; }
  };
  const resolved = require.resolve(path.join(__dirname, t.file));
  delete require.cache[resolved];
  require(resolved);
  if (!capturedAdapter) {
    console.error('FAIL ' + t.name + ': adapter did not register');
    failed++;
    continue;
  }
  try {
    const result = capturedAdapter.extract(dom.window.document);
    if (!result || typeof result !== 'object') throw new Error('extract returned non-object');
    if (!Array.isArray(result.items)) throw new Error('items is not an array');
    console.log('OK   ' + t.name + ': items=' + result.items.length +
      ' sourceID=' + JSON.stringify(result.sourceID) +
      ' sourceName=' + JSON.stringify(result.sourceName));
  } catch (e) {
    console.error('FAIL ' + t.name + ': ' + e.message);
    failed++;
  }
}

process.exit(failed ? 1 : 0);
