const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const {JSDOM}=require('jsdom');
const flush=()=>new Promise(resolve=>setImmediate(resolve));
async function popup(url,{fail=false}={}) {
 const dom=new JSDOM(fs.readFileSync(__dirname+'/../popup.html','utf8'),{runScripts:'outside-only'});
 const w=dom.window;let boards=['programming'];const sent=[];
 w.__rssPalReddit={getBoards:async()=>boards,detectSubreddit:address=>address.includes('/r/golang')?'golang':null};
 w.chrome={tabs:{query:async()=>[{url}]},storage:{local:{get:async()=>({})},onChanged:{addListener(){}}},runtime:{sendMessage:async message=>{if(message.action==='redditDiscoverySync')return{ok:true};sent.push(message);if(fail)return{ok:false,error:'需要管理员 Token'};if(message.action==='redditDiscoveryRemove')boards=boards.filter(b=>b!==message.subreddit);else boards.push(message.subreddit);return{ok:true};}}};
 w.eval(fs.readFileSync(__dirname+'/popup.js','utf8'));await flush();
 return {dom,w,sent};
}
test('popup detects current board, adds once and shows subscribed status',async()=>{
 const {dom,w,sent}=await popup('https://www.reddit.com/r/golang/comments/abc');
 assert.equal(w.document.getElementById('redditCurrent').style.display,'block');
 assert.match(w.document.getElementById('redditCurrentName').textContent,/r\/golang/);
 w.document.getElementById('redditAdd').click();await flush();
 assert.equal(sent.length,1);assert.equal(sent[0].subreddit,'golang');
 assert.equal(w.document.getElementById('redditAdd').textContent,'已加入探索');
 assert.equal(w.document.getElementById('redditAdd').disabled,true);
 w.document.getElementById('redditAdd').click();assert.equal(sent.length,1);dom.window.close();
});
test('popup hides add action outside subreddit and exposes registration errors',async()=>{
 let p=await popup('https://example.com');assert.equal(p.w.document.getElementById('redditCurrent').style.display,'none');p.dom.window.close();
 p=await popup('https://www.reddit.com/r/golang',{fail:true});p.w.document.getElementById('redditAdd').click();await flush();
 assert.match(p.w.document.getElementById('redditAddStatus').textContent,/管理员/);assert.equal(p.w.document.getElementById('redditAdd').disabled,false);p.dom.window.close();
});

test('popup list supports removal and restores add action for current community',async()=>{
 const {dom,w,sent}=await popup('https://www.reddit.com/r/golang');
 w.document.getElementById('redditAdd').click();await flush();
 w.document.querySelector('[aria-label="移除 r/golang"]').click();await flush();
 assert.equal(sent[1].action,'redditDiscoveryRemove');assert.equal(w.document.getElementById('redditAdd').disabled,false);dom.window.close();
});
