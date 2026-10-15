const { test } = require('node:test');
const assert = require('node:assert/strict');
const { postKey, advance, createHandler } = require('./reddit-reminder');
test('recognizes posts on supported Reddit hosts and dedupes comment permalinks', () => {
    assert.equal(postKey('https://old.reddit.com/r/programming/comments/abc123/title/comment/?sort=top'), 'abc123');
    for (const u of ['https://www.reddit.com/r/programming/', 'https://www.reddit.com/search/?q=comments', 'https://reddit.com.evil/comments/abc123/', 'https://user@reddit.com/comments/abc123/'])
        assert.equal(postKey(u), null);
});
test('draws only once after five minutes and honors the 60% boundary', () => {
    let draws = 0;
    let p = { elapsed: 299000 };
    p = advance(p, 999, () => { draws++; return .1; });
    assert.equal(draws, 0);
    p = advance(p, 1, () => { draws++; return .5999; });
    assert.equal(p.show, true);
    assert.equal(draws, 1);
    p = advance(p, 5000, () => { throw Error('reroll'); });
    assert.equal(p.show, false);
    assert.equal(advance({ elapsed: 299999 }, 1, () => .6).show, false);
});
function harness(random = () => 0) {
    const data = {};
    const sync = { serverUrl: 'https://rss.example', token: 'test-secret' };
    const requests = [];
    const chrome = { storage: { local: { get: async (k) => ({ [k]: data[k] }), set: async (x) => Object.assign(data, x) }, sync: { get: async () => sync, set: async (x) => Object.assign(sync, x) } }, tabs: { get: async (id) => ({ id, active: true, url: 'https://www.reddit.com/r/test/comments/abc123/title/' }) }, windows: { get: async () => ({ focused: true }) } };
    const handle = createHandler({ chrome, random, fetch: async (url, opts) => { requests.push({ url, opts }); return { ok: true, json: async () => ({ status: 'created', article_id: 42 }) }; } });
    const sender = { tab: { id: 1, windowId: 1 }, url: 'https://www.reddit.com/r/test/comments/abc123/title/', frameId: 0 };
    return { handle, sender, data, sync, requests, chrome };
}
test('persists progress and decision, and closing disables every post permanently', async () => {
    const h = harness();
    for (let i = 0; i < 59; i++)
        assert.equal((await h.handle({ action: 'redditReminderTick', elapsedMs: 5000 }, h.sender)).show, false);
    assert.equal((await h.handle({ action: 'redditReminderTick', elapsedMs: 5000 }, h.sender)).show, true);
    assert.equal((await h.handle({ action: 'redditReminderTick', elapsedMs: 5000 }, h.sender)).show, false);
    await h.handle({ action: 'redditReminderDisable' }, h.sender);
    assert.equal(h.sync.redditRemindersDisabled, true);
    assert.equal((await h.handle({ action: 'redditReminderTick', elapsedMs: 5000 }, h.sender)).disabled, true);
});
test('save uses verified sender URL and server-side credentials; no automatic overwrite', async () => {
    const h = harness();
    const r = await h.handle({ action: 'redditReminderSave', html: '<article>loaded discussion</article>', title: 'Title', url: 'https://evil.example' }, h.sender);
    assert.equal(r.articleURL, 'https://rss.example/articles/42');
    const req = h.requests[0];
    assert.equal(req.url, 'https://rss.example/api/bookmarklet/capture');
    const body = JSON.parse(req.opts.body);
    assert.equal(postKey(body.url), 'abc123');
    assert.equal(body.force, false);
        assert.equal(body.skip_existing, true);
    assert.equal(req.opts.headers.Authorization, 'Bearer test-secret');
    assert.equal(JSON.stringify(r).includes('test-secret'), false);
    assert.equal((await h.handle({ action: 'redditReminderTick', elapsedMs: 5000 }, { ...h.sender, url: 'https://evil.example', frameId: 1 })).ok, false);
});
test('never evicts a final probability decision when progress cache fills',async()=>{
 const h=harness(()=>{throw Error('must not reroll')});const posts={abc123:{decided:true}};for(let i=0;i<2100;i++)posts['p'+i]={decided:true};h.data.redditReminderPosts=posts;
 assert.equal((await h.handle({action:'redditReminderTick',elapsedMs:5000},h.sender)).show,false);assert.equal(h.data.redditReminderPosts.abc123.decided,true);
});
test('background tabs and unfocused windows never advance viewing time',async()=>{
 const h=harness();
 h.chrome.tabs.get=async()=>({active:false,url:h.sender.url});
 await h.handle({action:'redditReminderTick',elapsedMs:5000},h.sender);assert.equal(h.data.redditReminderPosts,undefined);
 h.chrome.tabs.get=async()=>({active:true,url:h.sender.url});h.chrome.windows.get=async()=>({focused:false});
 await h.handle({action:'redditReminderTick',elapsedMs:5000},h.sender);assert.equal(h.data.redditReminderPosts,undefined);
 assert.equal((await h.handle({action:'redditReminderSave',html:'<p>body</p>'},{...h.sender,frameId:1})).ok,false);assert.equal(h.requests.length,0);
});
