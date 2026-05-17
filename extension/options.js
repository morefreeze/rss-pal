(function () {
  'use strict';

  const $ = (id) => document.getElementById(id);

  const serverUrlInput = $('serverUrl');
  const tokenInput = $('token');
  const saveBtn = $('saveBtn');
  const testBtn = $('testBtn');
  const statusEl = $('status');

  function showStatus(type, msg) {
    statusEl.className = 'options-status show';
    statusEl.className += type === 'success' ? ' status-success' :
                           type === 'error' ? ' status-error' : ' status-warning';
    statusEl.textContent = msg;
  }

  function hideStatus() {
    statusEl.className = 'options-status';
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

  // Load saved settings
  async function loadSettings() {
    const data = await chrome.storage.sync.get(['serverUrl', 'token']);
    if (data.serverUrl) serverUrlInput.value = data.serverUrl;
    if (data.token) tokenInput.value = data.token;
  }

  // Save settings
  saveBtn.addEventListener('click', async () => {
    hideStatus();
    const serverUrl = serverUrlInput.value.trim();
    const token = tokenInput.value.trim();

    if (!serverUrl) {
      showStatus('error', '请输入服务器地址');
      serverUrlInput.focus();
      return;
    }

    if (!token) {
      showStatus('error', '请输入 Token');
      tokenInput.focus();
      return;
    }

    try {
      await chrome.storage.sync.set({ serverUrl, token });
      showStatus('success', '✅ 设置已保存');
    } catch (err) {
      showStatus('error', '❌ 保存失败：' + err.message);
    }
  });

  // Test connection
  testBtn.addEventListener('click', async () => {
    hideStatus();
    const serverUrl = serverUrlInput.value.trim();

    if (!serverUrl) {
      showStatus('error', '请先输入服务器地址');
      return;
    }

    setLoading(testBtn, true);

    try {
      // Try fetching a health/status endpoint
      const resp = await fetch(serverUrl.replace(/\/+$/, '') + '/api/health', {
        method: 'GET',
        signal: AbortSignal.timeout(10000),
      });

      if (resp.ok) {
        showStatus('success', '✅ 连接成功');
      } else {
        showStatus('warning', '⚠️ 服务器响应 HTTP ' + resp.status);
      }
    } catch (err) {
      if (err.name === 'TimeoutError' || err.name === 'AbortError') {
        showStatus('error', '❌ 连接超时，请检查服务器地址');
      } else {
        showStatus('error', '❌ 连接失败：' + err.message);
      }
    } finally {
      setLoading(testBtn, false);
    }
  });

  loadSettings();
})();
