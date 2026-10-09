const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
function load(extra={}) {
 const context = vm.createContext({ URL, AbortSignal, Date, console, ...extra });
 vm.runInContext(fs.readFileSync(__dirname+'/discovery.js','utf8'),context);
 return context.__rssPalReddit;
}
test('browser listing preserves numeric boundary and drops private/unneeded data', async()=>{
 const posts=[99,100,101].map((score,i)=>({kind:'t3',data:{id:'p'+i,score,subreddit:'programming',url:'https://blog.example/'+i,author:'secret-author',selftext:'not uploaded'}}));
 let requested;
 const api=load({location:{origin:'https://www.reddit.com'},fetch:async(url,opts)=>{requested={url,opts};return{ok:true,status:200,json:async()=>({kind:'Listing',data:{children:posts}})}}});
 const result=await api.readListing('programming','week');
 assert.equal(result.data.children.length,2);
 assert.equal(result.data.children[0].data.score,100);
 assert.equal(result.data.children[0].data.author,undefined);
 assert.equal(result.data.children[0].data.selftext,undefined);
 assert.equal(requested.opts.credentials,'include');
 assert.match(requested.url,/t=week/);
});
test('403 and malformed listing are failures, not successful empty discovery',async()=>{
 for (const response of [{ok:false,status:403},{ok:true,json:async()=>({error:403})}]) {
  const api=load({location:{origin:'https://www.reddit.com'},fetch:async()=>response});
  await assert.rejects(()=>api.readListing('programming','week'));
 }
});
test('foreign origins cannot read subreddit listings',async()=>{
 let calls=0;
 const api=load({location:{origin:'https://evil.example'},fetch:async()=>{calls++;}});
 await assert.rejects(()=>api.readListing('programming','week'));
 await assert.rejects(()=>api.readListing('unknown','week'));
 assert.equal(calls,0);
});

function harness({response,collectError,registrationError}={}) {
 const local={}; const session={};let stamp=1791500000000;let captures=0,uploads=0,closed=0,registrations=0;
 const store=data=>({get:async keys=>{if(typeof keys==='string')keys=[keys];return Object.fromEntries(keys.filter(k=>k in data).map(k=>[k,structuredClone(data[k])]));},set:async values=>Object.assign(data,structuredClone(values)),remove:async key=>{delete data[key];}});
 const cfg={serverUrl:'https://rss.example',token:'test-only-token'};
 const chromeApi={storage:{local:store(local),sync:store(cfg),session:store(session)},tabs:{create:async()=>{captures++;return{id:7};},get:async()=>({status:'complete'}),remove:async()=>{closed++;},onUpdated:{addListener(){},removeListener(){}}},scripting:{executeScript:async()=>{if(collectError)throw new Error(collectError);return[{result:{kind:'Listing',data:{children:[]}}}];}}};
 const api=load({fetch:()=>{},setTimeout,clearTimeout,TextEncoder,crypto:require('node:crypto').webcrypto});
 const options={chromeApi,now:()=>stamp,fetchImpl:async(url)=>{if(url.endsWith("/reddit-subreddits")){registrations++;return registrationError?{ok:false,status:403}:{ok:true,json:async()=>({subreddit:"golang"})};}uploads++;return response || {ok:true,json:async()=>({accepted:0,stats:{external_posts:0}})};}};
 return {collector:api.createCollector(options),recreate:()=>api.createCollector(options),local,cfg,advance:ms=>{stamp+=ms;},counts:()=>({captures,uploads,closed,registrations})};
}
test('disabled collector is idle; manual run processes eight lists once across restarts',async()=>{
 const h=harness();await h.collector.tick();assert.equal(h.counts().captures,0);
 await h.collector.requestRun();await h.collector.tick();
 const restarted=h.recreate();for(let i=0;i<9;i++){h.advance(60000);await restarted.tick();}
 assert.deepEqual(h.counts(),{captures:8,uploads:8,closed:8,registrations:0});
 assert.equal(Object.values(h.local.redditDiscovery.jobs).filter(j=>j.status==='done').length,8);
});
test('upload failures persist and retry without refetching; changing account clears pending data',async()=>{
 const h=harness({response:{ok:false,status:503}});await h.collector.requestRun();await h.collector.tick();
 assert.ok(h.local.redditDiscovery.jobs['programming:week'].pending);
 h.advance(5*60*1000);await h.collector.tick();assert.equal(h.counts().captures,1);assert.equal(h.counts().uploads,2);
 h.cfg.token='new-account';await h.collector.tick();
 // A new collection is permissible; the old account batch must not be uploaded.
 assert.equal(h.counts().captures,2);
});
test('six-hour scheduling and disabled toggle prevent automatic new reads',async()=>{
 const h=harness();await h.collector.configure(true);for(let i=0;i<8;i++){h.advance(60000);await h.collector.tick();}
 await h.collector.tick();assert.equal(h.counts().captures,8);
 h.advance(6*60*60*1000);await h.collector.tick();assert.equal(h.counts().captures,9);
 await h.collector.configure(false);await h.collector.tick();assert.equal(h.counts().captures,9);
});
test('403 capture records failure, closes owned tab and never uploads',async()=>{
 const h=harness({collectError:'Reddit HTTP 403'});await h.collector.requestRun();await h.collector.tick();
 assert.deepEqual(h.counts(),{captures:1,uploads:0,closed:1,registrations:0});
 assert.equal(h.local.redditDiscovery.jobs['programming:week'].status,'failed');
});
test('concurrent ticks collect at most one listing',async()=>{
 const h=harness();await h.collector.requestRun();await Promise.all([h.collector.tick(),h.collector.tick(),h.collector.tick()]);
 assert.equal(h.counts().captures,1);
});
test('interrupted collection resumes and repeated manual actions respect minute throttle',async()=>{
 const h=harness();await h.collector.requestRun();await h.collector.tick();
 await h.collector.requestRun();await h.collector.tick();assert.equal(h.counts().captures,1);
 const jobs=h.local.redditDiscovery.jobs;
 // Simulate a process stopped after persisting collecting but before capture finished.
 jobs['programming:week']={requested:false,nextAt:1791500000000+6*3600000,status:'collecting'};
 const restarted=h.recreate();h.advance(60000);await restarted.tick();
 assert.equal(h.counts().captures,2);assert.equal(h.local.redditDiscovery.jobs['programming:week'].status,'done');
});

test('detect subreddit on listing and post pages; reject aggregate/spoofed URLs',()=>{
 const api=load();
 for(const url of ['https://www.reddit.com/r/Golang/','https://old.reddit.com/r/golang/comments/abc/title','https://reddit.com/r/golang/top/?t=week'])assert.equal(api.detectSubreddit(url),'golang');
 for(const url of ['https://reddit.com.evil.test/r/golang','https://www.reddit.com/r/all','https://www.reddit.com/r/popular','https://www.reddit.com/r/go+rust','https://www.reddit.com/user/someone','https://www.reddit.com/r/u_someone'])assert.equal(api.detectSubreddit(url),null);
});
test('adding a subreddit registers once, persists and schedules its two lists while auto is off',async()=>{
 const h=harness();await Promise.all([h.collector.addSubreddit('GoLang'),h.collector.addSubreddit('golang')]);
 const subs={boards:Object.values(h.local.redditDiscoverySubscriptions.byOwner)[0]};
 assert.equal(subs.boards.length,1);assert.equal(subs.boards[0].name,'golang');
 assert.equal(h.counts().registrations,1);
 const restarted=h.recreate();await restarted.tick();h.advance(60000);await restarted.tick();h.advance(60000);await restarted.tick();
 assert.equal(h.counts().captures,2);
 assert.equal(h.local.redditDiscovery.jobs['golang:week'].status,'done');
 assert.equal(h.local.redditDiscovery.jobs['golang:month'].status,'done');
});
test('failed registration leaves list untouched',async()=>{
 const h=harness({registrationError:true});await assert.rejects(()=>h.collector.addSubreddit('golang'));
 assert.equal(h.local.redditDiscoverySubscriptions,undefined);
});

test('switching accounts preserves separate subreddit lists', async()=>{
 const h=harness();await h.collector.addSubreddit('golang');
 h.cfg.token='account-B';await h.collector.addSubreddit('golang');
 assert.equal(Object.keys(h.local.redditDiscoverySubscriptions.byOwner).length,2);
 h.cfg.token='test-only-token';await h.collector.addSubreddit('golang');
 assert.equal(h.counts().registrations,2);
});
