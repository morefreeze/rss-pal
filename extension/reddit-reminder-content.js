(function () {
    'use strict';
    const api = globalThis.__rssPalRedditReminder;
    if (!api || globalThis.__rssPalRedditReminderStarted)
        return;
    globalThis.__rssPalRedditReminderStarted = true;
    let key = api.postKey(location.href), last = performance.now(), wasActive = false, pending = 0, busy = false, disabled = false, host = null;
    function remove() { if (host) {
        host.remove();
        host = null;
    } }
    function show() {
        if (host || disabled)
            return;
        host = document.createElement('div');
        host.id = 'rss-pal-reddit-reminder';
        const shadow = host.attachShadow({ mode: 'closed' });
        const style = document.createElement('style');
        style.textContent = ':host{all:initial;position:fixed;right:20px;bottom:20px;z-index:2147483647;font:14px/1.5 system-ui,sans-serif;color:#202124}section{box-sizing:border-box;background:white;border:1px solid #dadce0;border-radius:12px;padding:16px;box-shadow:0 4px 24px #0003;max-width:min(340px,calc(100vw - 40px))}p{margin:0 0 12px}button,a{font:inherit}button{cursor:pointer;border:1px solid #dadce0;border-radius:6px;padding:6px 12px;background:#fff;color:#202124}button.save{background:#2563eb;color:white;border-color:#2563eb}button.close{float:right;border:0;padding:0 4px;margin-left:8px}a{color:#2563eb}small{display:block;margin-top:8px}';
        const shownKey = key;
        const section = document.createElement('section');
        section.setAttribute('role', 'region');
        section.setAttribute('aria-label', 'RSS Pal 保存提醒');
        const close = document.createElement('button');
        close.className = 'close';
        close.textContent = '×';
        close.title = '永久关闭所有帖子提醒';
        close.setAttribute('aria-label', '永久关闭所有帖子提醒');
        const text = document.createElement('p');
        text.textContent = '这篇帖子看了 5 分钟，保存到 RSS Pal？';
        const save = document.createElement('button');
        save.className = 'save';
        save.textContent = '保存';
        const status = document.createElement('small');
        status.setAttribute('role', 'status');
        close.onclick = async () => { close.disabled = true; try {
            const r = await chrome.runtime.sendMessage({ action: 'redditReminderDisable' });
            if (!r?.ok)
                throw Error();
            disabled = true;
            remove();
        }
        catch (_) {
            close.disabled = false;
            status.textContent = '关闭失败，请重试';
        } };
        save.onclick = async () => {
            if (api.postKey(location.href) !== shownKey) { remove(); return; }
            save.disabled = true;
            status.textContent = '保存中…';
            try {
                const clone = document.documentElement.cloneNode(true);
                clone.querySelectorAll('script,style,noscript,iframe,#rss-pal-reddit-reminder').forEach(n => n.remove());
                const r = await chrome.runtime.sendMessage({ action: 'redditReminderSave', title: document.title, html: clone.outerHTML });
                if (!r?.ok)
                    throw Error(r?.error || '保存失败，请重试');
                status.textContent = r.existing ? '已在 RSS Pal 中，原内容保持不变。' : '已保存到 RSS Pal。';
                save.remove();
                const link = document.createElement('a');
                link.textContent = '查看帖子';
                link.href = r.articleURL;
                link.target = '_blank';
                link.rel = 'noopener noreferrer';
                section.append(link);
            }
            catch (e) {
                save.disabled = false;
                status.textContent = e.message || '保存失败，请重试';
            }
        };
        const hint = document.createElement('small');
        hint.textContent = '关闭后不再提醒任何帖子';
        section.append(close, text, save, hint, status);
        shadow.append(style, section);
        document.documentElement.append(host);
    }
    async function tick() {
        const current = api.postKey(location.href), now = performance.now();
        if (current !== key) {
            key = current;
            pending = 0;
            wasActive = false;
            remove();
        }
        const active = !!key && document.visibilityState === 'visible' && document.hasFocus();
        if (active && wasActive)
            pending += Math.min(1500, Math.max(0, now - last));
        last = now;
        wasActive = active;
        if (disabled || busy || !active || pending < 5000)
            return;
        const elapsedMs = Math.min(6000, Math.floor(pending));
        pending = 0;
        busy = true;
        const sentKey = key;
        try {
            const r = await chrome.runtime.sendMessage({ action: 'redditReminderTick', elapsedMs });
            if (r?.disabled) {
                disabled = true;
                remove();
            }
            else if (r?.show && key === sentKey && api.postKey(location.href) === sentKey && document.visibilityState === 'visible' && document.hasFocus())
                show();
        }
        catch (_) { /* Extension reloads must not affect the page. */ }
        finally {
            busy = false;
        }
    }
    chrome.storage.onChanged.addListener((changes, area) => { if (area === 'sync' && changes.redditRemindersDisabled?.newValue === true) {
        disabled = true;
        remove();
    } });
    document.addEventListener('visibilitychange', tick);
    window.addEventListener('focus', tick);
    window.addEventListener('blur', tick);
    setInterval(tick, 1000);
})();
