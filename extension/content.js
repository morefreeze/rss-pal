// RSS Pal Content Script
// Listens for capture requests from popup/background and returns cleaned HTML.
// Injected at document_idle for mp.weixin.qq.com (and other sites via programmatic injection).

(function () {
  'use strict';

  function extractGenericHtml() {
    var clone = document.documentElement.cloneNode(true);
    clone.querySelectorAll('script,style,link,noscript,template').forEach(function (n) {
      n.remove();
    });
    clone.querySelectorAll('img').forEach(function (img) {
      var src = (img.getAttribute('src') || '').trim();
      if (!src || src.indexOf('data:') === 0) {
        var lazy = img.getAttribute('data-src') || img.getAttribute('data-original') ||
          img.getAttribute('data-actual-src') || img.getAttribute('data-lazy-src') ||
          img.getAttribute('data-original-src');
        if (lazy) img.setAttribute('src', lazy.trim());
      }
      if (!img.getAttribute('srcset')) {
        var ss = img.getAttribute('data-srcset') || img.getAttribute('data-lazy-srcset');
        if (ss) img.setAttribute('srcset', ss.trim());
      }
    });
    return clone.outerHTML;
  }

  /**
   * Extract background-image URLs from elements and convert to <img> tags.
   */
  function extractBgImages(container) {
    var imgs = [];
    container.querySelectorAll('[style*="background"]').forEach(function (el) {
      var style = el.getAttribute('style') || '';
      var match = style.match(/background-image\s*:\s*url\(["']?([^"')]+)["']?\)/);
      if (match && match[1] && match[1].indexOf('mmbiz') !== -1) {
        var url = match[1].split('?')[0] || match[1];
        // Remove /0, /300 etc. suffix to get full-resolution image
        url = url.replace(/\/\d+$/, '/0');
        imgs.push(url);
      }
    });
    return imgs;
  }

  var WX_NOISE = [
    'script', 'style', 'noscript',
    '.qr_code_pc', '.js_pc_qr_code', '#js_pc_close_btn',
    '.reward_area', '.reward_bottom_area', '#js_reward_area', '#js_pc_reward_btn',
    '#js_reward_container', '.reward_avatar', '.reward_tips', '.reward_user',
    '.share_area', '.social_area', '.media_tool_meta',
    '.comment_area', '.comment_area_primary', '#js_comment_area',
    '#js_tags_preview', '#js_tags_toast',
    '#js_bottom_popular_articles', '.rich_media_tool_area', '.js_tool_area',
    '#content_bottom_area', '.readmore_btn', '#js_share_read_more',
    '.profile_container', '#js_profile_qrcode', '#js_pc_follow_btn',
    '#js_reward_outer', '#js_other_reward', '#js_reward_intro',
    '.reward_area_wrapper', '.reward_other_input', '.reward_btn_area',
    '#js_view_source', '.rich_media_meta_list', '#js_tags_preview_container',
    '.weui-dialog', '.weui-mask', '.weui-half-screen',
    '.weui-actionsheet', '.weui-toast',
    'mpvoice', 'mpvideosnap', 'mpprofile', 'mpcommon', 'mpcheck',
    '#js_share_content_page_hd .swiper_indicator' // carousel indicators (dots)
  ];

  function cleanWxClone(clone) {
    // Promote lazy-loaded images
    clone.querySelectorAll('img').forEach(function (img) {
      var dataSrc = img.getAttribute('data-src');
      if (dataSrc) img.setAttribute('src', dataSrc.trim());
      if (!img.getAttribute('src') || (img.getAttribute('src') || '').indexOf('data:') === 0) {
        var lazy = img.getAttribute('data-original') || img.getAttribute('data-actual-src') ||
          img.getAttribute('data-lazy-src') || img.getAttribute('data-original-src');
        if (lazy) img.setAttribute('src', lazy.trim());
      }
      if (!img.getAttribute('srcset')) {
        var ss = img.getAttribute('data-srcset') || img.getAttribute('data-lazy-srcset');
        if (ss) img.setAttribute('srcset', ss.trim());
      }
    });

    // Remove noise
    WX_NOISE.forEach(function (sel) {
      clone.querySelectorAll(sel).forEach(function (n) { n.remove(); });
    });
    clone.querySelectorAll('[style*="display:none"],[style*="display: none"]').forEach(function (n) { n.remove(); });
    clone.querySelectorAll('mpvoice,mpvideosnap,mpprofile,mpcommon,mpcheck').forEach(function (n) { n.remove(); });
    clone.querySelectorAll('a[href^="javascript"]').forEach(function (a) {
      var text = document.createTextNode(a.textContent);
      a.parentNode.replaceChild(text, a);
    });
    // Remove <script> and <style>
    clone.querySelectorAll('script,style').forEach(function (n) { n.remove(); });
  }

  function extractWechatHtml() {
    var contentEl = document.querySelector('#js_content');
    if (!contentEl) return null;

    var clone = contentEl.cloneNode(true);
    cleanWxClone(clone);

    // Collect background-image URLs from #page_top_area (carousel images outside #js_content)
    var extraImages = [];
    var topArea = document.querySelector('#page_top_area');
    if (topArea) {
      extraImages = extractBgImages(topArea);
    }
    // Also check for bg-images inside the content itself
    var contentBgImages = extractBgImages(contentEl);
    extraImages = extraImages.concat(contentBgImages);

    // Build image HTML
    var imgHtml = extraImages.map(function (url) {
      return '<img src="' + url + '" alt="">';
    }).join('\n');

    // Title
    var title = document.querySelector('#activity-name') ||
                document.querySelector('#js_text_title') ||
                document.querySelector('.rich_media_title');
    var titleText = title ? title.textContent.trim() : '';

    var html = '<!DOCTYPE html><html><head><meta charset="utf-8"><title>' +
      titleText.replace(/</g, '&lt;').replace(/>/g, '&gt;') +
      '</title></head><body>' +
      '<div id="js_content">' +
      (imgHtml ? imgHtml : '') +
      clone.innerHTML +
      '</div></body></html>';

    return html;
  }

  function extractCleanHtml() {
    if (location.hostname === 'mp.weixin.qq.com') {
      var wxHtml = extractWechatHtml();
      if (wxHtml) return wxHtml;
    }
    return extractGenericHtml();
  }

  chrome.runtime.onMessage.addListener(function (request, sender, sendResponse) {
    if (request.action === 'captureHtml') {
      try {
        var html = extractCleanHtml();
        sendResponse({ html: html });
      } catch (err) {
        sendResponse({ error: err.message });
      }
      return true;
    }
  });
})();

// === Adapter dispatch (R4 passive path) ===
// Runs only on sites where a matching adapter has self-registered via
// window.__rssPalAdapters.register(...). On mp.weixin.qq.com there is no
// such adapter, so this IIFE is a no-op there.
(function () {
  'use strict';
  if (!window.__rssPalAdapters) return;  // registry not loaded

  // On-demand extract for popup's "⚡ 抓取本页" button. Registered globally
  // so it works regardless of whether a passive adapter is also active on
  // this page (popup probes via this message before showing the button).
  chrome.runtime.onMessage.addListener(function (msg, _sender, sendResponse) {
    if (!msg || msg.action !== 'getCurrentExtract') return;
    try {
      const ad = window.__rssPalAdapters.findFor(location);
      if (!ad) { sendResponse({ matched: false }); return; }
      const result = ad.extract(document) || {};
      sendResponse({
        matched: true,
        site: ad.site,
        name: ad.name,
        sourceKind: ad.sourceKind,
        sourceID: result.sourceID || '',
        sourceName: result.sourceName || '',
        items: Array.isArray(result.items) ? result.items : [],
      });
    } catch (e) {
      sendResponse({ matched: false, error: String(e && e.message || e) });
    }
    return false; // synchronous response
  });

  const adapter = window.__rssPalAdapters.findFor(location);
  if (!adapter || !adapter.passive) return;

  // Map adapter.sourceKind → options toggle key. Defaults: list/user/bookmarks ON.
  // Note: twitter:home has no adapter yet; the twHomeEnabled toggle is inert
  // until a home adapter registers itself (it'll wire up automatically).
  const TOGGLE_KEY_BY_KIND = {
    'twitter:list': 'twListEnabled',
    'twitter:user': 'twUserEnabled',
    'twitter:bookmarks': 'twBookmarksEnabled',
    'twitter:home': 'twHomeEnabled',
  };

  async function runExtractAndQueue() {
    let count = -1;  // -1 = "didn't run / unknown"; >=0 = real extract count
    try {
      // Honor per-source auto-extract toggle from options page.
      const toggleKey = TOGGLE_KEY_BY_KIND[adapter.sourceKind];
      if (toggleKey) {
        const data = await chrome.storage.sync.get([toggleKey]);
        // Home defaults OFF; everything else defaults ON.
        if (toggleKey === 'twHomeEnabled') {
          if (!data[toggleKey]) return;
        } else {
          if (data[toggleKey] === false) return;
        }
      }
      const result = adapter.extract(document);
      count = (result && Array.isArray(result.items)) ? result.items.length : 0;
      if (!result || !count) return;
      const cfg = await chrome.storage.sync.get(['serverUrl', 'token']);
      if (!cfg.serverUrl || !cfg.token) return;
      if (!window.__rssPalQueue) return;
      await window.__rssPalQueue.push({
        source_kind: adapter.sourceKind,
        source_id: result.sourceID,
        source_name: result.sourceName,
        items: result.items,
      });
      // Auto-discover: upsert into known_sources, refreshing last_seen so the
      // popup dropdown can sort most-recently-visited first. Cap the list at
      // KNOWN_SOURCES_MAX so it doesn't grow unbounded — evict the oldest by
      // last_seen when full.
      const KNOWN_SOURCES_MAX = 30;
      const known = await chrome.storage.sync.get(['known_sources']);
      const list = Array.isArray(known.known_sources) ? known.known_sources : [];
      const key = adapter.sourceKind + '/' + result.sourceID;
      const now = Date.now();
      const existing = list.find((s) => s.key === key);
      if (existing) {
        existing.last_seen = now;
        if (result.sourceName) existing.source_name = result.sourceName;
      } else {
        list.push({
          key,
          source_kind: adapter.sourceKind,
          source_id: result.sourceID,
          source_name: result.sourceName,
          discovered_at: now,
          last_seen: now,
        });
      }
      list.sort((a, b) => (b.last_seen || b.discovered_at || 0) - (a.last_seen || a.discovered_at || 0));
      if (list.length > KNOWN_SOURCES_MAX) list.length = KNOWN_SOURCES_MAX;
      await chrome.storage.sync.set({ known_sources: list });
      // Trigger background flush (best effort)
      try {
        const p = chrome.runtime.sendMessage({ action: 'flushQueue' });
        if (p && typeof p.catch === 'function') p.catch(() => {});
      } catch (_) {
        // ignore — background may be asleep
      }
    } catch (e) {
      console.warn('[rss-pal adapter]', adapter.name, 'extract failed:', e);
    } finally {
      // Always update the per-tab badge so the user can see live whether the
      // adapter found anything on this tab (0 is useful info too — "we see
      // this page, we just don't see any items yet"). Background sets it
      // scoped to sender.tab.id, so it won't clobber other tabs.
      if (count >= 0) {
        try {
          const p = chrome.runtime.sendMessage({ action: 'updateBadge', count });
          if (p && typeof p.catch === 'function') p.catch(() => {});
        } catch (_) {}
      }
    }
  }

  // First extract on document_idle
  runExtractAndQueue();

  // Re-extract on DOM mutations (debounced)
  let debTimer = null;
  const debounce = (fn, ms) => {
    if (debTimer) clearTimeout(debTimer);
    debTimer = setTimeout(fn, ms);
  };
  if (document.body) {
    new MutationObserver(() => debounce(runExtractAndQueue, 800))
      .observe(document.body, { childList: true, subtree: true });
  }
})();
