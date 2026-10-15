// Shared by the isolated Reddit content script and the service worker.
(function (root) {
    'use strict';
    function postKey(raw) { try {
        const u = new URL(raw);
        if (u.protocol !== 'https:' || u.username || u.password || !['reddit.com', 'www.reddit.com', 'old.reddit.com', 'new.reddit.com'].includes(u.host))
            return null;
        return u.pathname.match(/^\/(?:r\/[^/]+\/)?comments\/([a-z0-9]+)(?:\/|$)/i)?.[1].toLowerCase() || null;
    }
    catch (_) {
        return null;
    } }
    function advance(previous, delta, random) { const p = { ...previous, show: false }; if (p.decided)
        return p; p.elapsed = Math.min(300000, Math.max(0, p.elapsed || 0) + Math.min(6000, Math.max(0, Number(delta) || 0))); if (p.elapsed >= 300000) {
        p.decided = true;
        p.show = random() < 0.6;
    } return p; }
    function createHandler({ chrome, fetch, random = Math.random, now = Date.now }) {
        let queue = Promise.resolve();
        async function process(msg, sender) {
            const id = postKey(sender?.url);
            if (!id || !sender.tab || sender.frameId !== 0)
                return { ok: false };
            const cfg = await chrome.storage.sync.get(['redditRemindersDisabled', 'serverUrl', 'token']);
            if (msg.action === 'redditReminderDisable') {
                await chrome.storage.sync.set({ redditRemindersDisabled: true });
                return { ok: true, disabled: true };
            }
            if (cfg.redditRemindersDisabled)
                return { ok: true, disabled: true, show: false };
            if (msg.action === 'redditReminderTick') {
                if (!cfg.serverUrl || !cfg.token)
                    return { ok: true, show: false };
                const tab = await chrome.tabs.get(sender.tab.id);
                const win = await chrome.windows.get(sender.tab.windowId);
                if (!tab.active || !win.focused || postKey(tab.url) !== id)
                    return { ok: true, show: false };
                const { redditReminderPosts: stored } = await chrome.storage.local.get('redditReminderPosts');
                const posts = stored && typeof stored === 'object' ? stored : {};
                const p = advance(posts[id], msg.elapsedMs, random);
                posts[id] = p.decided ? {decided:true} : {elapsed:p.elapsed,touched:now()};
                // Only incomplete progress may expire; final decisions are never re-rolled.
                const keys = Object.keys(posts).filter(k => !posts[k].decided);
                if (keys.length > 2000)
                    keys.sort((a, b) => (posts[a].touched || 0) - (posts[b].touched || 0)).slice(0, keys.length - 2000).forEach(k => delete posts[k]);
                await chrome.storage.local.set({ redditReminderPosts: posts });
                return { ok: true, show: p.show };
            }
            if (msg.action === 'redditReminderSave') {
                if (!cfg.serverUrl || !cfg.token)
                    return { ok: false, error: '请先配置 RSS Pal 插件' };
                if (typeof msg.html !== 'string' || !msg.html || new TextEncoder().encode(JSON.stringify({ html: msg.html })).length > 3.5 * 1024 * 1024)
                    return { ok: false, error: '页面内容过大，请使用插件网摘按钮' };
                const base = cfg.serverUrl.replace(/\/+$/, '');
                const response = await fetch(base + '/api/bookmarklet/capture', { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + cfg.token }, body: JSON.stringify({ url: sender.url, title: String(msg.title || '').slice(0, 1000), html: msg.html, force: false, force_new: false, skip_existing: true }), signal: AbortSignal.timeout(35000) });
                const result = await response.json();
                if (!response.ok)
                    return { ok: false, error: result.error || '保存失败，请重试' };
                if (!Number.isInteger(result.article_id) || result.article_id <= 0)
                    return { ok: false, error: '保存结果无效，请使用插件网摘按钮确认' };
                return { ok: true, existing: result.status === 'duplicate', articleURL: base + '/articles/' + result.article_id };
            }
            return { ok: false };
        }
        return (msg, sender) => { const result = queue.then(() => process(msg, sender)); queue = result.catch(() => { }); return result; };
    }
    const api = { postKey, advance, createHandler };
    root.__rssPalRedditReminder = api;
    if (typeof module !== 'undefined')
        module.exports = api;
})(globalThis);
