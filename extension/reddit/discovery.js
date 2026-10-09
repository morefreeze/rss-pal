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
        (typeof subreddit !== 'string' || !/^[a-z0-9_]{1,21}$/i.test(subreddit) || /^u_/i.test(subreddit) || ['all','popular','friends','mod'].includes(subreddit.toLowerCase())) ||
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

  function normalizeSubreddit(name) {
    if (typeof name !== 'string') return null;
    name=name.toLowerCase();
    return /^[a-z0-9_]{1,21}$/.test(name) && !name.startsWith('u_') &&
      !['all','popular','friends','mod'].includes(name) ? name : null;
  }
  function detectSubreddit(address) {
    try {
      const url=new URL(address);
      if (!['https:','http:'].includes(url.protocol) ||
          !['reddit.com','www.reddit.com','old.reddit.com','new.reddit.com','m.reddit.com'].includes(url.hostname)) return null;
      const match=url.pathname.match(/^\/r\/([^/]+)(?:\/|$)/i);
      return match ? normalizeSubreddit(match[1]) : null;
    } catch (_) {return null;}
  }
  async function configOwner(cfg) {
    const digest=await crypto.subtle.digest('SHA-256',new TextEncoder().encode(cfg.serverUrl+'\n'+cfg.token));
    return Array.from(new Uint8Array(digest),b=>b.toString(16).padStart(2,'0')).join('');
  }
  function validateConfig(cfg) {
    if (!cfg.serverUrl || !cfg.token) throw new Error('请先配置服务器和 Token');
    const url=new URL(cfg.serverUrl);
    if (url.protocol !== 'https:' && !(url.protocol === 'http:' && ['localhost','127.0.0.1'].includes(url.hostname))) throw new Error('Reddit 探索需要 HTTPS 服务器');
  }
  async function subscriptions(chromeApi, owner) {
    const data=(await chromeApi.storage.local.get('redditDiscoverySubscriptions')).redditDiscoverySubscriptions;
    const boards=data?.byOwner?.[owner] || (data?.owner === owner ? data.boards : null);
    return Array.isArray(boards) ? boards : [];
  }
  async function getBoards(chromeApi) {
    const cfg=await chromeApi.storage.sync.get(['serverUrl','token']);
    const added=await subscriptions(chromeApi,await configOwner(cfg));
    return [...BOARDS.map(name=>name.toLowerCase()),...added.map(b=>b.name)];
  }

  function createCollector({ chromeApi, fetchImpl = fetch, now = Date.now }) {
    let busy = false;
    let registration=Promise.resolve();
    function addSubreddit(raw) {
      // Serialize additions separately from tick's state writes so a pending
      // network fetch cannot overwrite a newly added community.
      const operation=registration.then(async()=>{
        const name=normalizeSubreddit(raw);
        if (!name) throw new Error('当前页面不是可探索的 subreddit');
        const cfg=await chromeApi.storage.sync.get(['serverUrl','token']);
        validateConfig(cfg);
        const owner=await configOwner(cfg);
        const boards=await subscriptions(chromeApi,owner);
        if (BOARDS.some(b=>b.toLowerCase()===name) || boards.some(b=>b.name===name)) return name;
        if (boards.length>=50) throw new Error('最多添加 50 个 subreddit');
        const response=await fetchImpl(cfg.serverUrl.replace(/\/+$/,'')+'/api/extension/reddit-subreddits',{
          method:'POST',headers:{'Content-Type':'application/json',Authorization:'Bearer '+cfg.token},
          body:JSON.stringify({subreddit:name}),signal:AbortSignal.timeout(20000),
        });
        if (!response.ok) throw new Error(response.status===403?'需要 RSS Pal 管理员 Token':'加入探索失败：HTTP '+response.status);
        const result=await response.json();
        if (result.subreddit!==name) throw new Error('服务器返回了无效 subreddit');
        const latest=await chromeApi.storage.sync.get(['serverUrl','token']);
        if (await configOwner(latest)!==owner) throw new Error('配置已更改，请重试');
        boards.push({name,addedAt:now()});
        const stored=(await chromeApi.storage.local.get('redditDiscoverySubscriptions')).redditDiscoverySubscriptions;
        const byOwner={...stored?.byOwner};
        if (stored?.owner && Array.isArray(stored.boards)) byOwner[stored.owner]=stored.boards;
        byOwner[owner]=boards;
        await chromeApi.storage.local.set({redditDiscoverySubscriptions:{byOwner}});
        return name;
      });
      registration=operation.catch(()=>{});
      return operation;
    }
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
        try {validateConfig(cfg);} catch(e) {s.error=e.message;await save(s);return;}
        const owner=await configOwner(cfg);
        if (s.owner !== owner) s={owner,enabled:s.enabled,jobs:{}};
        s.error=null;
        const added=await subscriptions(chromeApi,owner);
        const jobs=[...JOBS];
        for (const board of added) {
          if (!normalizeSubreddit(board.name) || BOARDS.some(b=>b.toLowerCase()===board.name)) continue;
          for (const period of ['week','month']) {
            const key=board.name+':'+period;
            jobs.push({key,subreddit:board.name,period});
            const v=s.jobs[key] ||= {};
            if (!v.addedAt || v.addedAt < board.addedAt) {v.addedAt=board.addedAt;v.requested=true;}
          }
        }
        // A persisted collecting state without a payload means Chrome exited
        // before the fetch completed. Restore intent even in manual mode.
        for (const v of Object.values(s.jobs)) {
          if (v.status === 'collecting' && !v.pending) {v.requested=true;v.status='interrupted';}
        }
        if (manual > (s.requestedAt || 0)) {
          s.requestedAt=manual;
          const active=Object.values(s.jobs).some(v=>v.requested || v.pending);
          if (!active) for (const job of jobs) {s.jobs[job.key] ||= {};s.jobs[job.key].requested=true;}
        }
        if (s.nextTickAt && s.nextTickAt > now()) {await save(s);return;}
        // One listing per alarm tick. Persisted checkpoints survive SW suspension.
        const job = jobs.find(j => {
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
    return {tick,configure,requestRun,state,addSubreddit};
  }
  globalThis.__rssPalReddit = { readListing, createCollector, JOBS, detectSubreddit, getBoards };
})();
