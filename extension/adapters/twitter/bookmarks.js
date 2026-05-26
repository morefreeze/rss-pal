// extension/adapters/twitter/bookmarks.js
//
// Independent DOM scraper for x.com's /i/bookmarks page. Reads rendered DOM
// in the user's logged-in Chrome tab and emits TweetItem records matching
// the shape produced by OpenCLI's GraphQL-based adapter
// (https://github.com/jackwener/opencli, clis/twitter/bookmarks.js).
//
// OpenCLI's adapter calls Twitter's /i/api/graphql/<queryId>/Bookmarks
// directly with an authenticated ct0 cookie + Bearer token (`Strategy.COOKIE`).
// That approach doesn't translate to MV3 content scripts, which can read the
// rendered DOM but cannot recover the Bearer token without main-world script
// injection. So this adapter independently extracts the same TweetItem
// fields (id, author, text, created_at, url, ...) from rendered article
// elements instead, keeping downstream backend normalization uniform across
// twitter sources.
//
// We follow OpenCLI's adapter directory layout (extension/adapters/<site>/<command>.js),
// registry pattern, and output schema for compatibility with their docs and
// the future possibility of contributing back a DOM-mode variant upstream.
//
// Last reviewed: 2026-05-26.
// When OpenCLI's adapter commits churn (signal of x.com DOM changes), revisit
// our selectors. See docs/extension-adapters/upstream-map.md.

(function () {
  'use strict';

  function parseCount(s) {
    if (!s) return 0;
    const trimmed = String(s).trim().replace(/,/g, '');
    if (!trimmed) return 0;
    const m = trimmed.match(/^([\d.]+)\s*([KMB])?/i);
    if (!m) return 0;
    let n = parseFloat(m[1]);
    if (isNaN(n)) return 0;
    const suffix = (m[2] || '').toUpperCase();
    if (suffix === 'K') n *= 1e3;
    else if (suffix === 'M') n *= 1e6;
    else if (suffix === 'B') n *= 1e9;
    return Math.round(n);
  }

  function parseStatusHref(href) {
    if (!href) return null;
    const m = href.match(/^\/?([^/]+)\/status\/(\d+)/);
    if (!m) return null;
    return { author: m[1], id: m[2] };
  }

  function extractTweetArticle(article) {
    if (!article) return null;
    let parsed = null;
    const links = article.querySelectorAll('a[href*="/status/"]');
    for (const a of links) {
      const p = parseStatusHref(a.getAttribute('href') || '');
      if (p) { parsed = p; break; }
    }
    if (!parsed) return null;
    const author = parsed.author.toLowerCase();
    const id = parsed.id;

    let displayName = '';
    const userNameBlock = article.querySelector('[data-testid="User-Name"]');
    if (userNameBlock) {
      const spans = userNameBlock.querySelectorAll('span');
      for (const sp of spans) {
        const t = (sp.textContent || '').trim();
        if (t && !t.startsWith('@') && t !== '·') { displayName = t; break; }
      }
    }

    let text = '';
    const textEl = article.querySelector('[data-testid="tweetText"]');
    if (textEl) text = (textEl.textContent || '').trim();

    let createdAt = '';
    const timeEl = article.querySelector('time[datetime]');
    if (timeEl) createdAt = timeEl.getAttribute('datetime') || '';

    const mediaUrls = [];
    article.querySelectorAll('img[src*="twimg.com/media"]').forEach((img) => {
      const src = img.getAttribute('src');
      if (src) mediaUrls.push(src);
    });
    article.querySelectorAll('video[poster]').forEach((v) => {
      const poster = v.getAttribute('poster');
      if (poster) mediaUrls.push(poster);
    });

    let quotedUrl = '';
    const innerStatusLinks = article.querySelectorAll('div[role="link"] a[href*="/status/"]');
    for (const a of innerStatusLinks) {
      const href = a.getAttribute('href') || '';
      const p = parseStatusHref(href);
      if (p && p.id !== id) {
        quotedUrl = 'https://x.com' + (href.startsWith('/') ? href : '/' + href);
        break;
      }
    }

    function countFor(testId) {
      const btn = article.querySelector('[data-testid="' + testId + '"]');
      if (!btn) return 0;
      const label = btn.getAttribute('aria-label') || '';
      const m = label.match(/([\d.,]+)\s*[KMB]?/i);
      if (m) return parseCount(m[0]);
      const sp = btn.querySelector('span');
      if (sp) return parseCount(sp.textContent || '');
      return 0;
    }
    const replies = countFor('reply');
    const retweets = countFor('retweet');
    const likes = countFor('like');

    let views = 0;
    const analyticsLink = article.querySelector('a[href*="/analytics"]');
    if (analyticsLink) {
      const label = analyticsLink.getAttribute('aria-label') || analyticsLink.textContent || '';
      const m = label.match(/([\d.,]+)\s*[KMB]?/i);
      if (m) views = parseCount(m[0]);
    }

    return {
      id,
      author,
      display_name: displayName,
      text,
      created_at: createdAt,
      url: 'https://x.com/' + author + '/status/' + id,
      media_urls: mediaUrls,
      quoted_url: quotedUrl,
      likes,
      retweets,
      replies,
      views,
    };
  }

  function extract(document) {
    const loc = document.location || (document.defaultView && document.defaultView.location);
    const pathname = loc ? loc.pathname : '';
    if (!/^\/i\/bookmarks/.test(pathname)) {
      return { items: [], sourceID: '', sourceName: '', hasMore: false };
    }

    const sourceID = 'self';
    const sourceName = '我的 Bookmarks';

    const items = [];
    const seen = new Set();
    const articles = document.querySelectorAll('article[data-testid="tweet"]');
    for (const art of articles) {
      let item;
      try {
        item = extractTweetArticle(art);
      } catch (_) {
        continue;
      }
      if (!item || !item.id) continue;
      if (seen.has(item.id)) continue;
      seen.add(item.id);
      items.push(item);
    }

    return { items, sourceID, sourceName, hasMore: false };
  }

  if (globalThis.__rssPalAdapters) {
    globalThis.__rssPalAdapters.register({
      site: 'twitter',
      name: 'bookmarks',
      sourceKind: 'twitter:bookmarks',
      domain: 'x.com',
      urlPattern: /^\/i\/bookmarks/,
      pullable: true,
      passive: true,
      extract,
    });
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = { extract };
  }
})();
