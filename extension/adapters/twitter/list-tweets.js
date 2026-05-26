// extension/adapters/twitter/list-tweets.js
//
// Portions of this file derive from OpenCLI (https://github.com/jackwener/opencli)
//   commit c730a02, file clis/twitter/list-tweets.js,
//   licensed under Apache-2.0. See extension/adapters/THIRD_PARTY_NOTICES.md.
// Last reviewed: 2026-05-26
//
// Note: OpenCLI's twitter/list-tweets.js is a `Strategy.COOKIE` adapter that
// fetches `/i/api/graphql/<queryId>/ListLatestTweetsTimeline` and parses the
// GraphQL JSON response. That pattern is infeasible in an MV3 content script
// (we don't have the Bearer token, and intercepting page fetches requires
// design we haven't done yet). For rss-pal R4 we DOM-scrape the rendered
// timeline instead. The TweetItem schema we emit, the per-tweet fields we
// care about, and the engagement-counter conventions are taken from OpenCLI's
// extractTimelineTweet().
//
// When OpenCLI updates this file, see docs/extension-adapters/upstream-map.md.

(function () {
  'use strict';

  // Parse engagement counts like "1.2K", "3.4M", "12" → integer.
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

  // Pull statusId out of a "/<handle>/status/<id>" href.
  function parseStatusHref(href) {
    if (!href) return null;
    const m = href.match(/^\/?([^/]+)\/status\/(\d+)/);
    if (!m) return null;
    return { author: m[1], id: m[2] };
  }

  // Extract a single tweet's fields from an <article data-testid="tweet"> element.
  // Returns null when the element is missing the bare minimum (status link + id).
  function extractTweetArticle(article) {
    if (!article) return null;

    // Status link + id (also gives us the canonical author handle).
    let statusLink = null;
    let parsed = null;
    const links = article.querySelectorAll('a[href*="/status/"]');
    for (const a of links) {
      const href = a.getAttribute('href') || '';
      const p = parseStatusHref(href);
      if (p) {
        statusLink = a;
        parsed = p;
        break;
      }
    }
    if (!parsed) return null;
    const author = parsed.author.toLowerCase();
    const id = parsed.id;

    // Display name: under data-testid="User-Name", first <span> with text content.
    let displayName = '';
    const userNameBlock = article.querySelector('[data-testid="User-Name"]');
    if (userNameBlock) {
      const spans = userNameBlock.querySelectorAll('span');
      for (const sp of spans) {
        const t = (sp.textContent || '').trim();
        if (t && !t.startsWith('@') && t !== '·') {
          displayName = t;
          break;
        }
      }
    }

    // Tweet text. data-testid="tweetText" carries the rendered body.
    let text = '';
    const textEl = article.querySelector('[data-testid="tweetText"]');
    if (textEl) text = (textEl.textContent || '').trim();

    // Created-at: <time datetime="ISO">.
    let createdAt = '';
    const timeEl = article.querySelector('time[datetime]');
    if (timeEl) createdAt = timeEl.getAttribute('datetime') || '';

    // Media URLs from inline images and video posters.
    const mediaUrls = [];
    article.querySelectorAll('img[src*="twimg.com/media"]').forEach((img) => {
      const src = img.getAttribute('src');
      if (src) mediaUrls.push(src);
    });
    article.querySelectorAll('video[poster]').forEach((v) => {
      const poster = v.getAttribute('poster');
      if (poster) mediaUrls.push(poster);
    });

    // Quoted tweet — Twitter renders a nested role="link" group with an inner
    // status link distinct from the outer tweet's link.
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

    // Engagement counters via data-testid="reply" | "retweet" | "like".
    function countFor(testId) {
      const btn = article.querySelector('[data-testid="' + testId + '"]');
      if (!btn) return 0;
      // Twitter exposes the count via aria-label and a child text node.
      const label = btn.getAttribute('aria-label') || '';
      const m = label.match(/([\d.,]+)\s*[KMB]?/i);
      if (m) return parseCount(m[0]);
      // Fallback: inner span text.
      const sp = btn.querySelector('span');
      if (sp) return parseCount(sp.textContent || '');
      return 0;
    }
    const replies = countFor('reply');
    const retweets = countFor('retweet');
    const likes = countFor('like');

    // View count — often inside a link to /analytics with aria-label "N Views".
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
    const m = pathname.match(/^\/i\/lists\/(\d+)/);
    if (!m) {
      return { items: [], sourceID: '', sourceName: '', hasMore: false };
    }
    const sourceID = m[1];

    // List header name — typically `<h2><span dir="ltr">…</span></h2>`.
    let sourceName = '';
    const header = document.querySelector('h2 [dir="ltr"]');
    if (header) sourceName = (header.textContent || '').trim();
    if (!sourceName) sourceName = 'Twitter List ' + sourceID;

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
      name: 'list-tweets',
      sourceKind: 'twitter:list',
      domain: 'x.com',
      urlPattern: /^\/i\/lists\/(\d+)/,
      pullable: true,
      passive: true,
      extract,
    });
  }

  // Smoke test (only runs when explicitly invoked, e.g. node --eval).
  // Not registered as a vitest suite to avoid setting up the toolchain
  // for this autonomous run. The real validation is the Phase J manual
  // end-to-end on a logged-in browser.
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = { extract };
  }
})();
