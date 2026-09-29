const {test} = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const bridge = fs.readFileSync(__dirname+'/bridge-content.js','utf8');
const consent = fs.readFileSync(__dirname+'/consent.js','utf8');
test('bridge rejects foreign origin/source and never forwards consent or credential payloads',async()=>{
 let receive; const messages=[]; const replies=[];
 const window={addEventListener:(_,cb)=>{receive=cb;},postMessage:data=>replies.push(data)}; window.top=window;
 const origin='https://rss.morefreeze.top';
 vm.runInNewContext(bridge,{window,location:{origin},chrome:{runtime:{sendMessage:async msg=>{messages.push(msg);return {ok:true,cookies:['secret'],token:'secret'};}}}});
 const data={source:'rss-pal-englife-page',type:'PING',requestId:'id'};
 for (const event of [{source:window,origin:'https://evil.test',data},{source:{},origin,data},{source:window,origin,data:{...data,type:'CONFIRM'}},{source:window,origin,data:{...data,type:'AUTHORIZE',serverOrigin:'https://evil.test'}}]) await receive(event);
 assert.equal(messages.length,0);
 await receive({source:window,origin,data});
 assert.equal(messages.length,1); assert.deepEqual(JSON.parse(JSON.stringify(replies)),[{source:'rss-pal-englife-extension',requestId:'id',ok:true}]);
});
test('consent page opening only reads public context; denied permission cancels without confirming',async()=>{
 const elements={}; const messages=[]; let requests=0;
 for (const id of ['confirm','cancel','status','destination']) elements[id]={disabled:id==='confirm',addEventListener:(_,cb)=>{elements[id].click=cb;}};
 vm.runInNewContext(consent,{URLSearchParams,location:{search:'?id=opaque'},document:{getElementById:id=>elements[id]},chrome:{runtime:{sendMessage:async msg=>{messages.push(msg);return {ok:true,origin:'https://rss.morefreeze.top'};}},permissions:{request:async()=>{requests++;return false;}}}});
 await Promise.resolve(); await Promise.resolve();
 assert.equal(requests,0); assert.deepEqual(messages.map(m=>m.type),['CONTEXT']);
 await elements.confirm.click();
 assert.equal(requests,1); assert.deepEqual(messages.map(m=>m.type),['CONTEXT','CANCEL']);
 assert.match(elements.status.textContent,/未授予权限/);
});
test('manifest cookies permission is optional and englife bridge origins are constrained',()=>{
 const manifest=JSON.parse(fs.readFileSync(__dirname+'/../manifest.json','utf8'));
 assert.ok(!manifest.permissions.includes('cookies')); assert.ok(manifest.optional_permissions.includes('cookies'));
 assert.deepEqual(manifest.content_scripts.find(s=>s.js.includes('englife/bridge-content.js')).matches,['https://rss.morefreeze.top/*','http://localhost:5173/*','http://127.0.0.1:5173/*']);
});
