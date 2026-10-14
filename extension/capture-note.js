// Private, per-account/page drafts. Writes are dispatched on every input so
// closing a popup does not depend on an unload handler or debounce timer.
(function () {
  'use strict';
  window.createCaptureNoteForm = async function ({ tab, serverUrl, token, onSubmit }) {
    const input = document.getElementById('captureNote');
    const fieldset = document.getElementById('captureNoteFields');
    const hint = document.getElementById('captureNoteDraft');
    const count = document.getElementById('captureNoteCount');
    const quote = document.getElementById('quoteSelection');
    const max = 2000;
    const scope = JSON.stringify([serverUrl.replace(/\/+$/, ''), token, tab.url]);
    const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(scope));
    const key = 'captureNoteDraft:' + Array.from(new Uint8Array(digest), n => n.toString(16).padStart(2, '0')).join('');
    const pending = new Set();
    let locked = false;
    let selection = '';
    function updateCount() {
      const length = Array.from(input.value).length;
      count.textContent = length + ' / ' + max;
      input.setCustomValidity(length > max ? '随手记最多 2000 字，请精简后保存。' : '');
    }
    function persist() {
      updateCount();
      hint.textContent = '正在保留草稿…';
      const value = input.value;
      const request = chrome.storage.local.set({ [key]: value });
      pending.add(request);
      request.then(() => {
        if (input.value === value) hint.textContent = value ? '草稿已保留，关掉也不丢' : '选填，仅自己可见';
      }, () => { hint.textContent = '草稿暂未保留，请保持弹窗打开后重试'; })
        .finally(() => pending.delete(request));
    }
    function append(text) {
      if (locked) return;
      const prefix = input.value && !input.value.endsWith('\n') ? '\n' : '';
      const next = input.value + prefix + text;
      if (Array.from(next).length > max) {
        hint.textContent = '加上这段会超过 2000 字，可以精简后再试';
        input.focus();
        return;
      }
      input.value = next;
      persist();
      input.focus();
      input.setSelectionRange(input.value.length, input.value.length);
    }
    try {
      const data = await chrome.storage.local.get([key]);
      input.value = typeof data[key] === 'string' ? data[key] : '';
      hint.textContent = input.value ? '已恢复上次未保存的想法' : '选填，仅自己可见';
    } catch (_) {
      hint.textContent = '暂时无法读取草稿，请保持弹窗打开';
    }
    updateCount();
    input.addEventListener('input', persist);
    input.addEventListener('keydown', event => {
      if (event.key === 'Enter' && (event.metaKey || event.ctrlKey) && !event.isComposing) {
        event.preventDefault();
        if (!locked) onSubmit();
      }
    });
    document.querySelectorAll('[data-note-prompt]').forEach(button => {
      button.addEventListener('click', () => append(button.dataset.notePrompt));
    });
    quote.addEventListener('click', () => {
      if (selection) append(selection.split('\n').map(line => '> ' + line).join('\n') + '\n');
    });
    // Reading selected text is optional and must not delay ordinary saving.
    if (tab.id && /^https?:/.test(tab.url || '')) {
      chrome.scripting.executeScript({ target: { tabId: tab.id }, func: () => window.getSelection()?.toString() || '' })
        .then(results => {
          selection = String(results?.[0]?.result || '').trim();
          if (selection) {
            quote.hidden = false;
            quote.title = selection.slice(0, 180);
          }
        }).catch(() => {});
    }
    fieldset.disabled = false;
    return {
      matches: (url, server, credential) => url === tab.url && server.replace(/\/+$/, '') === serverUrl.replace(/\/+$/, '') && credential === token,
      value: () => input.value.trim(),
      valid: () => input.reportValidity(),
      lock(value) { locked = value; fieldset.disabled = value; },
      async finish(result, submitted) {
        if (!submitted) return '';
        if (result.note_saved !== true) return ' · 随手记尚未确认保存，草稿已保留，请更新服务后重试';
        if (input.value.trim() === submitted) {
          await Promise.allSettled([...pending]);
          try {
            await chrome.storage.local.remove(key);
            input.value = '';
            updateCount();
            hint.textContent = '想法已和网页一起保存';
          } catch (_) {
            hint.textContent = '想法已保存，本机草稿暂未清理';
          }
        }
        return ' · 随手记已保存';
      },
    };
  };
})();
