const { test } = require('node:test');
const assert = require('node:assert/strict');
const { createConnection } = require('./connection.js');
function fixture() {
 const data = {}; let now = 1000; const reads = []; const sent = [];
 const chrome = { runtime: { id: 'ours', getURL: p => 'chrome-extension://ours/' + p },
 storage: { session: { get: async key => key === null ? {...data} : ({[key]:data[key]}), set: async obj => Object.assign(data,obj), remove: async key => { delete data[key]; } } },
 tabs: { create: async () => ({ id: 42 }), update: async () => ({ id: 42 }) }, permissions: { contains: async () => true },
 cookies: { getAll: async query => { reads.push(query); return [{name:'session',value:'secret',domain:'.englife.space',path:'/',secure:true},{name:'google',value:'never',domain:'.google.com'},{name:'sub',value:'never',domain:'evil.englife.space'}]; } } };
 const connection = createConnection({chromeApi:chrome, now:() => now, randomID:()=>'random', fetchFn:async (url,options) => {sent.push({url,options}); return {ok:true};} });
 const sender = {id:'ours',frameId:0,tab:{id:3,url:'https://rss.morefreeze.top/admin/integrations/englife'},url:'https://rss.morefreeze.top/admin/integrations/englife'};
 const consent = {id:'ours',frameId:0,tab:{id:42,url:'chrome-extension://ours/englife/consent.html?id=random'},url:'chrome-extension://ours/englife/consent.html?id=random'};
 const authorize = () => connection.handle({type:'AUTHORIZE',serverOrigin:'https://rss.morefreeze.top',token:'pair'},sender);
 return {connection,chrome,reads,sent,sender,consent,authorize,expire:()=>{now+=601000;},data};
}
test('authorize never reads cookies; consent reads only englife and sends directly without bearer',async()=>{
 const f=fixture(); assert.equal((await f.authorize()).ok,true); assert.equal(f.reads.length,0);
 assert.equal((await f.connection.handle({type:'CONFIRM',id:'random'},f.consent)).ok,true);
 assert.deepEqual(f.reads,[{domain:'englife.space'}]);
 assert.equal(f.sent[0].url,'https://rss.morefreeze.top/api/integrations/englife/complete');
 assert.deepEqual(JSON.parse(f.sent[0].options.body).cookies,[{name:'session',value:'secret',domain:'.englife.space',path:'/',secure:true}]);
 assert.equal(f.sent[0].options.headers.Authorization,undefined);
 assert.equal((await f.connection.handle({type:'CONFIRM',id:'random'},f.consent)).ok,false);
});
test('rejects malicious sender, origin and iframe without opening consent',async()=>{
 for (const override of [{id:'other'},{frameId:1},{tab:{id:3,url:'https://evil.test/'}},{url:'https://evil.test/'}]) {
 const f=fixture(); assert.equal((await f.connection.handle({type:'AUTHORIZE',token:'pair',serverOrigin:'https://rss.morefreeze.top'},{...f.sender,...override})).ok,false); assert.deepEqual(f.data,{});
 }
 const f=fixture(); assert.equal((await f.connection.handle({type:'AUTHORIZE',token:'pair',serverOrigin:'https://evil.test'},f.sender)).ok,false);
});
test('page cannot consent; expiry and permission rejection cannot read cookies',async()=>{
 const f=fixture(); await f.authorize();
 assert.equal((await f.connection.handle({type:'CONFIRM',id:'random'},f.sender)).ok,false);
 f.expire(); assert.equal((await f.connection.handle({type:'CONFIRM',id:'random'},f.consent)).ok,false); assert.equal(f.reads.length,0);
 const g=fixture(); await g.authorize(); g.chrome.permissions.contains=async()=>false;
 assert.equal((await g.connection.handle({type:'CONFIRM',id:'random'},g.consent)).ok,false); assert.equal(g.reads.length,0);
});
test('consent context exposes only destination and rejects another tab',async()=>{
 const f=fixture(); await f.authorize();
 assert.deepEqual(await f.connection.handle({type:'CONTEXT',id:'random'},f.consent),{ok:true,origin:'https://rss.morefreeze.top'});
 assert.equal((await f.connection.handle({type:'CONTEXT',id:'random'},{...f.consent,tab:{...f.consent.tab,id:99}})).ok,false);
 assert.equal(f.reads.length,0);
});
test('concurrent confirmation and cleanup prevent replay and expired token retention',async()=>{
 const f=fixture(); await f.authorize();
 const results=await Promise.all([f.connection.handle({type:'CONFIRM',id:'random'},f.consent),f.connection.handle({type:'CONFIRM',id:'random'},f.consent)]);
 assert.equal(results.filter(r=>r.ok).length,1); assert.equal(f.sent.length,1);
 const g=fixture(); await g.authorize(); g.expire(); await g.connection.cleanup(); assert.deepEqual(g.data,{});
});
test('binding is stored before the consent page can load',async()=>{
 const f=fixture(); let createdURL; let context;
 f.chrome.tabs.create=async ({url})=>{createdURL=url;return {id:42};};
 f.chrome.tabs.update=async (id,{url})=>{assert.equal(id,42);assert.equal(url,f.consent.url);context=await f.connection.handle({type:'CONTEXT',id:'random'},f.consent);};
 assert.equal((await f.authorize()).ok,true);
 assert.equal(createdURL,'about:blank'); assert.equal(context.ok,true);
});
test('pair lifetime is ten minutes and completion allows 45 seconds',async t=>{
 const timeouts=[]; t.mock.method(AbortSignal,'timeout',ms=>{timeouts.push(ms);return new AbortController().signal;});
 const f=fixture(); await f.authorize();
 assert.equal(f.data['englife-pair-random'].expires,601000);
 await f.connection.handle({type:'CONFIRM',id:'random'},f.consent);
 assert.deepEqual(timeouts,[45000]);
});
