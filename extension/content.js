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
