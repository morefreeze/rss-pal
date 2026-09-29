const id = new URLSearchParams(location.search).get('id');
const confirmButton = document.getElementById('confirm');
const cancelButton = document.getElementById('cancel');
const status = document.getElementById('status');
confirmButton.addEventListener('click', async () => {
  confirmButton.disabled = true; cancelButton.disabled = true;
  try {
    // Preserve the explicit extension-page user gesture for permission request.
    const granted = await chrome.permissions.request({ permissions: ['cookies'] });
    if (!granted) {
      await chrome.runtime.sendMessage({ channel: 'englife', type: 'CANCEL', id });
      status.textContent = '未授予权限。请返回 RSS Pal 重新发起连接。'; return;
    }
    status.textContent = '正在连接…';
    const result = await chrome.runtime.sendMessage({ channel: 'englife', type: 'CONFIRM', id });
    status.textContent = result?.ok ? '连接完成，请返回 RSS Pal 查看状态。' : '连接失败或请求已过期，请返回 RSS Pal 重新连接。';
  } catch (_) { status.textContent = '连接失败，请返回 RSS Pal 重新连接。'; }
});
cancelButton.addEventListener('click', async () => {
  confirmButton.disabled = true; cancelButton.disabled = true;
  try { await chrome.runtime.sendMessage({ channel: 'englife', type: 'CANCEL', id }); } catch (_) {}
  status.textContent = '已取消授权。';
});

chrome.runtime.sendMessage({ channel: 'englife', type: 'CONTEXT', id }).then(result => {
  if (!result?.ok) { status.textContent = '请求已失效，请返回 RSS Pal 重新连接。'; return; }
  document.getElementById('destination').textContent = '接收登录状态的服务器：' + result.origin;
  confirmButton.disabled = false;
}).catch(() => { status.textContent = '无法读取请求，请返回 RSS Pal 重新连接。'; });
