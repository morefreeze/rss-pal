(function () {
  'use strict';

  const $ = (id) => document.getElementById(id);

  // Elements
  const notConfigured = $('notConfigured');
  const mainContent = $('mainContent');
  const pageTitle = $('pageTitle');
  const pageUrl = $('pageUrl');
  const captureBtn = $('captureBtn');
  const statusEl = $('status');
  const duplicatePrompt = $('duplicatePrompt');
  const dupMessage = $('dupMessage');
  const overwriteBtn = $('overwriteBtn');
  const newBtn = $('newBtn');
  const cancelBtn = $('cancelBtn');
  const settingsLink = $('settingsLink');
  const goSettings = $('goSettings');
  const autoConfigBtn = $('autoConfigBtn');
  const notConfiguredMsg = $('notConfiguredMsg');

  let detectedConfig = null;

  let currentPage = { url: '', title: '' };
  let lastCapture = null; // stores {url, title, html, serverUrl, token}

  // --- Helpers ---

  function showStatus(type, msg) {
    statusEl.className = 'status show status-' + type;
    statusEl.innerHTML = msg;
    statusEl.style.display = 'block';
  }

  function hideStatus() {
    statusEl.className = 'status';
    statusEl.style.display = 'none';
  }

  function hideDuplicate() {
    duplicatePrompt.style.display = 'none';
  }

  function escapeHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function buildArticleLink(serverUrl, articleId) {
    if (!articleId) return '';
    const base = String(serverUrl || '').replace(/\/+$/, '');
    if (!base) return '';
    const href = base + '/articles/' + encodeURIComponent(articleId);
    return ' · <a class="article-link" href="' + href +
      '" target="_blank" rel="noopener noreferrer">打开文章 ↗</a>';
  }

  function setLoading(btn, loading) {
    btn.disabled = loading;
    if (loading) {
      btn.dataset.origText = btn.textContent;
      btn.innerHTML = '<span class="loading"></span>' + btn.dataset.origText;
    } else {
      btn.textContent = btn.dataset.origText || btn.textContent;
    }
  }

  function openOptions() {
    chrome.runtime.openOptionsPage();
  }

  function isPDFTab(tab) {
    if (!tab || !tab.url) return false;
    const u = tab.url.split('?')[0].split('#')[0].toLowerCase();
    return u.endsWith('.pdf');
  }

  // --- Init ---

  async function init() {
    // Load config
    const data = await chrome.storage.sync.get(['serverUrl', 'token']);
    const serverUrl = (data.serverUrl || '').trim();
    const token = (data.token || '').trim();

    if (!serverUrl || !token) {
      notConfigured.style.display = 'block';
      mainContent.style.display = 'none';
      // Fire-and-forget: scripting.executeScript is slow on heavy pages and
      // would otherwise block the popup from appearing.
      detectConfigFromPage();
      return;
    }

    notConfigured.style.display = 'none';
    mainContent.style.display = 'block';

    // Get active tab info
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (tab) {
      currentPage.url = tab.url || '';
      currentPage.title = tab.title || '';
      pageTitle.textContent = currentPage.title || '(无标题)';
      pageUrl.textContent = currentPage.url;

      if (isPDFTab(tab)) {
        captureBtn.textContent = '⭐ 网摘此 PDF';
        captureBtn.dataset.mode = 'pdf';
      } else {
        captureBtn.dataset.mode = 'html';
      }
    }
  }

  async function detectConfigFromPage() {
    try {
      const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
      if (!tab || !tab.id || !tab.url || !tab.url.startsWith('http')) return;
      const results = await chrome.scripting.executeScript({
        target: { tabId: tab.id },
        func: () => window.__RSS_PAL_CONFIG || null,
      });
      const config = results && results[0] && results[0].result;
      if (config && config.serverUrl && config.token) {
        detectedConfig = config;
        notConfiguredMsg.textContent = '检测到当前页面的 RSS Pal 配置';
        autoConfigBtn.style.display = '';
      }
    } catch (e) {
      // Not on an RSS Pal page or no permission
    }
  }

  // --- Capture ---

  async function captureHtml(tabId) {
    // Try sending message to content script first (works if content.js is injected)
    try {
      const resp = await chrome.tabs.sendMessage(tabId, { action: 'captureHtml' });
      if (resp && resp.html) return resp.html;
    } catch (e) {
      // Content script not injected, fall through to programmatic injection
    }

    // Programmatically inject content script logic
    const results = await chrome.scripting.executeScript({
      target: { tabId },
      func: extractCleanHtml,
    });

    if (results && results[0] && results[0].result) {
      return results[0].result;
    }
    throw new Error('无法获取页面内容');
  }

  // This function runs in the page context via chrome.scripting.executeScript
  function extractCleanHtml() {
    if (location.hostname === 'mp.weixin.qq.com') {
      var contentEl = document.querySelector('#js_content');
      if (contentEl) {
        var clone = contentEl.cloneNode(true);
        // Promote lazy images
        clone.querySelectorAll('img').forEach(function (img) {
          var dataSrc = img.getAttribute('data-src');
          if (dataSrc) img.setAttribute('src', dataSrc.trim());
          if (!img.getAttribute('src') || (img.getAttribute('src')||'').indexOf('data:') === 0) {
            var lazy = img.getAttribute('data-original')||img.getAttribute('data-actual-src')||img.getAttribute('data-lazy-src')||img.getAttribute('data-original-src');
            if (lazy) img.setAttribute('src', lazy.trim());
          }
        });
        // Remove noise
        ['script','style','noscript','.qr_code_pc','.js_pc_qr_code','#js_pc_close_btn',
         '.reward_area','.reward_bottom_area','#js_reward_area','#js_pc_reward_btn',
         '#js_reward_container','.reward_avatar','.reward_tips','.reward_user',
         '.share_area','.social_area','.media_tool_meta',
         '.comment_area','.comment_area_primary','#js_comment_area',
         '#js_tags_preview','#js_tags_toast',
         '#js_bottom_popular_articles','.rich_media_tool_area','.js_tool_area',
         '#content_bottom_area','.readmore_btn','#js_share_read_more',
         '.profile_container','#js_profile_qrcode','#js_pc_follow_btn',
         '#js_reward_outer','#js_other_reward','#js_reward_intro',
         '.reward_area_wrapper','.reward_other_input','.reward_btn_area',
         '#js_view_source','.rich_media_meta_list',
         '.weui-dialog','.weui-mask','.weui-half-screen','.weui-actionsheet','.weui-toast',
         'mpvoice','mpvideosnap','mpprofile','mpcommon','mpcheck',
         '.swiper_indicator'
        ].forEach(function (sel) {
          clone.querySelectorAll(sel).forEach(function (n) { n.remove(); });
        });
        clone.querySelectorAll('a[href^="javascript"]').forEach(function (a) {
          var t = document.createTextNode(a.textContent); a.parentNode.replaceChild(t, a);
        });
        // Extract background-image URLs from #page_top_area (carousel images outside #js_content)
        var extraImages = [];
        var topArea = document.querySelector('#page_top_area');
        if (topArea) {
          topArea.querySelectorAll('[style*="background"]').forEach(function (el) {
            var m = (el.getAttribute('style')||'').match(/background-image\s*:\s*url\(["']?([^"')]+)["']?\)/);
            if (m && m[1] && m[1].indexOf('mmbiz') !== -1) { var u=m[1].split('?')[0]||m[1]; u=u.replace(/\/\d+$/,'/0'); extraImages.push(u); }
          });
        }
        var imgHtml = extraImages.map(function(u){return '<img src="'+u+'" alt="">';}).join('\n');
        var title = document.querySelector('#activity-name')||document.querySelector('#js_text_title');
        var titleText = title ? title.textContent.trim() : '';
        return '<!DOCTYPE html><html><head><meta charset="utf-8"><title>' +
          titleText.replace(/</g,'&lt;').replace(/>/g,'&gt;') +
          '</title></head><body>' +
          '<div id="js_content">' + (imgHtml ? imgHtml : '') + clone.innerHTML + '</div></body></html>';
      }
    }
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

  async function sendToServer(url, title, html, serverUrl, token, force, forceNew) {
    const resp = await fetch(serverUrl.replace(/\/+$/, '') + '/api/bookmarklet/capture', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer ' + token,
      },
      body: JSON.stringify({ url, title, html, force: !!force, force_new: !!forceNew }),
    });

    if (!resp.ok) {
      let errMsg = 'HTTP ' + resp.status;
      try {
        const errData = await resp.json();
        errMsg = errData.message || errData.error || errMsg;
      } catch (_) {}
      throw new Error(errMsg);
    }

    return await resp.json();
  }

  async function doCaptureHTML(tab, serverUrl, token, force) {
    const html = await captureHtml(tab.id);

    lastCapture = { url: currentPage.url, title: currentPage.title, html, serverUrl, token };

    const result = await sendToServer(currentPage.url, currentPage.title, html, serverUrl, token, force);

    if (result.status === 'duplicate' && !force) {
      hideStatus();
      const link = buildArticleLink(serverUrl, result.article_id);
      dupMessage.innerHTML = escapeHtml(result.message || '该文章已存在，是否覆盖？') + link;
      duplicatePrompt.style.display = 'block';
    } else {
      const link = buildArticleLink(serverUrl, result.article_id);
      showStatus('success', '✅ ' + escapeHtml(result.message || '发送成功') + link);
    }
  }

  async function doCapturePDF(tab, serverUrl, token) {
    const resp = await fetch(tab.url);
    if (!resp.ok) throw new Error('下载 PDF 失败：HTTP ' + resp.status);
    const blob = await resp.blob();

    const fd = new FormData();
    fd.append('url', tab.url);
    fd.append('title', tab.title || '');
    fd.append('file', blob, 'capture.pdf');

    const send = await fetch(serverUrl.replace(/\/+$/, '') + '/api/bookmarklet/capture-pdf', {
      method: 'POST',
      headers: { Authorization: 'Bearer ' + token },
      body: fd,
    });
    if (!send.ok) {
      let msg = 'HTTP ' + send.status;
      try {
        const j = await send.json();
        msg = j.message || j.error || msg;
      } catch (_) {}
      throw new Error(msg);
    }
    const result = await send.json();
    const link = buildArticleLink(serverUrl, result.article_id);
    if (result.status === 'processing') {
      showStatus('info', '⏳ ' + escapeHtml(result.message || 'PDF 已入库，OCR 处理中') + link);
    } else {
      showStatus('success', '✅ ' + escapeHtml(result.message || 'PDF 已加入网摘') + link);
    }
  }

  async function doCapture(force) {
    hideStatus();
    hideDuplicate();

    const data = await chrome.storage.sync.get(['serverUrl', 'token']);
    const serverUrl = (data.serverUrl || '').trim();
    const token = (data.token || '').trim();

    if (!serverUrl || !token) {
      openOptions();
      return;
    }

    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (!tab) return;

    setLoading(captureBtn, true);

    try {
      if (captureBtn.dataset.mode === 'pdf') {
        await doCapturePDF(tab, serverUrl, token);
      } else {
        await doCaptureHTML(tab, serverUrl, token, force);
      }
    } catch (err) {
      showStatus('error', '❌ ' + escapeHtml(err.message));
    } finally {
      setLoading(captureBtn, false);
    }
  }

  // --- Events ---

  captureBtn.addEventListener('click', () => doCapture(false));

  overwriteBtn.addEventListener('click', async () => {
    if (!lastCapture) return;
    hideDuplicate();
    setLoading(overwriteBtn, true);
    try {
      const result = await sendToServer(
        lastCapture.url, lastCapture.title, lastCapture.html,
        lastCapture.serverUrl, lastCapture.token, true
      );
      const link = buildArticleLink(lastCapture.serverUrl, result.article_id);
      showStatus('success', '✅ ' + escapeHtml(result.message || '覆盖成功') + link);
    } catch (err) {
      showStatus('error', '❌ ' + escapeHtml(err.message));
    } finally {
      setLoading(overwriteBtn, false);
    }
  });

  newBtn.addEventListener('click', async () => {
    if (!lastCapture) return;
    hideDuplicate();
    setLoading(newBtn, true);
    try {
      const result = await sendToServer(
        lastCapture.url, lastCapture.title, lastCapture.html,
        lastCapture.serverUrl, lastCapture.token, false, true
      );
      const link = buildArticleLink(lastCapture.serverUrl, result.article_id);
      showStatus('success', '✅ ' + escapeHtml(result.message || '已加入网摘') + link);
    } catch (err) {
      showStatus('error', '❌ ' + escapeHtml(err.message));
    } finally {
      setLoading(newBtn, false);
    }
  });

  cancelBtn.addEventListener('click', () => {
    hideDuplicate();
  });

  settingsLink.addEventListener('click', (e) => {
    e.preventDefault();
    openOptions();
  });

  goSettings.addEventListener('click', () => {
    openOptions();
  });

  autoConfigBtn.addEventListener('click', async () => {
    if (!detectedConfig) return;
    try {
      await chrome.storage.sync.set({
        serverUrl: detectedConfig.serverUrl,
        token: detectedConfig.token,
      });
      // Reload popup to show main content
      location.reload();
    } catch (err) {
      notConfiguredMsg.textContent = '配置失败：' + err.message;
    }
  });

  // Init
  init();
})();
