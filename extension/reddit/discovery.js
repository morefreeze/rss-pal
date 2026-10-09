// Independent implementation of browser-session listing reads. OpenCLI's
// subreddit adapter informed the approach; no bridge/runtime code is bundled.
(function () {
  'use strict';
  const BOARDS = ['programming', 'MachineLearning', 'LocalLLaMA', 'artificial'];
  const JOBS = BOARDS.flatMap(subreddit => ['week', 'month'].map(period => ({
    key: subreddit + ':' + period, subreddit, period,
  })));
  const SIX_HOURS = 6 * 60 * 60 * 1000;

  // Self-contained: Chrome serializes this function into the Reddit page.
  async function readListing(subreddit, period) {
    if (location.origin !== 'https://www.reddit.com' ||
        !['programming', 'MachineLearning', 'LocalLLaMA', 'artificial'].includes(subreddit) ||
        !['week', 'month'].includes(period)) throw new Error('Invalid Reddit collection target');
    const response = await fetch('/r/' + subreddit + '/top.json?t=' + period + '&limit=100&raw_json=1', {
      credentials: 'include', signal: AbortSignal.timeout(20000),
    });
    if (!response.ok) throw new Error('Reddit HTTP ' + response.status);
    const listing = await response.json();
    if (listing.kind !== 'Listing' || !Array.isArray(listing.data?.children) || listing.data.children.length > 100) {
      throw new Error('Reddit did not return a valid listing; login or access may be required');
    }
    const children = [];
    for (const entry of listing.data.children) {
      const d = entry.data;
      if (entry.kind !== 't3' || !d || typeof d.subreddit !== 'string' || d.subreddit.toLowerCase() !== subreddit.toLowerCase()) {
        throw new Error('Unexpected Reddit post or subreddit');
      }
      if (!Number.isSafeInteger(d.score) || d.score < 100) continue;
      const data = {};
      for (const name of ['id', 'score', 'subreddit', 'url', 'url_overridden_by_dest',
        'is_self', 'is_video', 'is_gallery', 'over_18', 'promoted']) {
        if (d[name] !== undefined) data[name] = d[name];
      }
      children.push({ kind: 't3', data });
    }
    return { kind: 'Listing', data: { children } };
  }

  function createCollector({ chromeApi, fetchImpl = fetch, now = Date.now }) {
    let busy = false;
    async function state() { return (await chromeApi.storage.local.get('redditDiscovery')).redditDiscovery || { enabled: false, jobs: {} }; }
    async function save(value) { await chromeApi.storage.local.set({ redditDiscovery: value }); }
    async function configure(enabled) {
      // Serialize setting changes with a running tick through a separate key.
      await chromeApi.storage.local.set({ redditDiscoveryEnabled: enabled === true });
    }
    async function requestRun() {
      await chromeApi.storage.local.set({ redditDiscoveryRequestedAt: now() });
    }
    async function waitForTab(tabId) {
      return new Promise((resolve, reject) => {
        let done = false;
        const finish = error => {
          if (done) return; done = true;
          clearTimeout(timer); chromeApi.tabs.onUpdated.removeListener(listener);
          error ? reject(error) : resolve();
        };
        const listener = (id, info) => { if (id === tabId && info.status === 'complete') finish(); };
        const timer = setTimeout(() => finish(new Error('Reddit tab load timed out')), 25000);
        chromeApi.tabs.onUpdated.addListener(listener);
        chromeApi.tabs.get(tabId).then(tab => { if (tab.status === 'complete') finish(); }, finish);
      });
    }
    async function cleanup() {
      const data = await chromeApi.storage.session.get('redditDiscoveryTab');
      if (Number.isInteger(data.redditDiscoveryTab)) {
        try { await chromeApi.tabs.remove(data.redditDiscoveryTab); } catch (_) {}
        await chromeApi.storage.session.remove('redditDiscoveryTab');
      }
    }
    async function collect(job) {
      await cleanup();
      const tab = await chromeApi.tabs.create({url:'https://www.reddit.com/r/'+job.subreddit+'/top/?t='+job.period,active:false});
      try {
        await chromeApi.storage.session.set({redditDiscoveryTab:tab.id});
        await waitForTab(tab.id);
        const results = await chromeApi.scripting.executeScript({ target:{tabId:tab.id},world:'MAIN',func:readListing,args:[job.subreddit,job.period] });
        const listing = results?.[0]?.result;
        if (!listing || listing.kind !== 'Listing' || !Array.isArray(listing.data?.children)) throw new Error('Reddit returned no listing');
        return {subreddit:job.subreddit,period:job.period,captured_at:new Date(now()).toISOString(),listing};
      } finally {
        try { await chromeApi.tabs.remove(tab.id); } catch (_) {}
        await chromeApi.storage.session.remove('redditDiscoveryTab');
      }
    }
    async function tick() {
      if (busy) return;
      busy = true;
      try {
        await cleanup();
        const cfg = await chromeApi.storage.sync.get(['serverUrl','token']);
        const settings = await chromeApi.storage.local.get(['redditDiscoveryEnabled','redditDiscoveryRequestedAt']);
        let s = await state();
        s.enabled = settings.redditDiscoveryEnabled === true;
        const manual = settings.redditDiscoveryRequestedAt || 0;
        if (!cfg.serverUrl || !cfg.token) {
          if (s.enabled || manual) {s.error='请先配置服务器和 Token';await save(s);}
          return;
        }
        const server = new URL(cfg.serverUrl);
        if (server.protocol !== 'https:' && !(server.protocol === 'http:' && ['localhost','127.0.0.1'].includes(server.hostname))) {
          s.error='Reddit 探索需要 HTTPS 服务器'; await save(s); return;
        }
        const digest = await crypto.subtle.digest('SHA-256',new TextEncoder().encode(cfg.serverUrl+'\n'+cfg.token));
        const owner = Array.from(new Uint8Array(digest),b=>b.toString(16).padStart(2,'0')).join('');
        if (s.owner !== owner) s={owner,enabled:s.enabled,jobs:{}};
        s.error=null;
        // A persisted collecting state without a payload means Chrome exited
        // before the fetch completed. Restore intent even in manual mode.
        for (const v of Object.values(s.jobs)) {
          if (v.status === 'collecting' && !v.pending) {v.requested=true;v.status='interrupted';}
        }
        if (manual > (s.requestedAt || 0)) {
          s.requestedAt=manual;
          const active=Object.values(s.jobs).some(v=>v.requested || v.pending);
          if (!active) for (const job of JOBS) {s.jobs[job.key] ||= {};s.jobs[job.key].requested=true;}
        }
        if (s.nextTickAt && s.nextTickAt > now()) {await save(s);return;}
        // One listing per alarm tick. Persisted checkpoints survive SW suspension.
        const job = JOBS.find(j => {
          const v=s.jobs[j.key] || {};
          return (v.pending && (!v.retryAt || v.retryAt <= now())) ||
            (!v.pending && (v.requested || (s.enabled && (!v.nextAt || v.nextAt <= now()))));
        });
        if (!job) {await save(s);return;}
        s.nextTickAt=now()+60000;
        const v=s.jobs[job.key] ||= {};
        if (!v.pending) {
          v.requested=false; v.nextAt=now()+SIX_HOURS; v.status='collecting'; v.error=null;
          await save(s);
          try {v.pending=await collect(job);v.qualified=v.pending.listing.data.children.length;}
          catch(e) {v.status='failed';v.error=String(e.message || e).slice(0,250);v.updatedAt=now();await save(s);return;}
          await save(s);
        }
        if (now()-Date.parse(v.pending.captured_at)>12*60*60*1000) {
          delete v.pending;v.status='failed';v.error='采集结果已过期，请重新探索';await save(s);return;
        }
        try {
          const response=await fetchImpl(cfg.serverUrl.replace(/\/+$/,'')+'/api/extension/reddit-discovery',{
            method:'POST',headers:{'Content-Type':'application/json',Authorization:'Bearer '+cfg.token},
            body:JSON.stringify(v.pending),signal:AbortSignal.timeout(20000),
          });
          if (!response.ok) {
            // Permanent rejections need a new capture/configuration, not a retry loop.
            if ([400,401,403,409,413].includes(response.status)) delete v.pending;
            throw new Error(response.status===403?'需要 RSS Pal 管理员 Token': '上传 HTTP '+response.status);
          }
          const result=await response.json();
          if (!Number.isInteger(result.accepted) || !result.stats) throw new Error('Invalid discovery response');
          v.accepted=result.accepted;v.external=result.stats.external_posts;v.duplicate=result.duplicate;
          v.status='done';v.error=null;delete v.pending;delete v.retryAt;
        } catch(e) {v.status='upload_failed';v.error=String(e.message || e).slice(0,250);v.retryAt=now()+5*60*1000;}
        v.updatedAt=now();
        await save(s);
      } finally {busy=false;}
    }
    return {tick,configure,requestRun,state};
  }
  globalThis.__rssPalReddit = { readListing, createCollector, JOBS };
})();
