(function () {
  'use strict';
  const toggle=document.getElementById('redditAuto');
  const button=document.getElementById('redditRun');
  const status=document.getElementById('redditStatus');
  const add=document.getElementById('redditAdd');
  const addStatus=document.getElementById('redditAddStatus');
  let currentSubreddit=null;
  async function renderCurrent() {
    const boards=await globalThis.__rssPalReddit.getBoards(chrome);
    const list=document.getElementById('redditBoards');list.replaceChildren();
    for(const name of boards) {
      const row=document.createElement('div');row.style.cssText='display:flex;justify-content:space-between;align-items:center;margin:6px 0';
      const label=document.createElement('span');label.textContent='r/'+name;
      const remove=document.createElement('button');remove.textContent='移除';remove.setAttribute('aria-label','移除 r/'+name);
      remove.addEventListener('click',async()=>{remove.disabled=true;try{await send({action:'redditDiscoveryRemove',subreddit:name});await renderCurrent();}catch(e){status.textContent=e.message;remove.disabled=false;}});
      row.append(label,remove);list.append(row);
    }
    if(!boards.length)list.textContent='尚未添加 subreddit';
    const [tab]=await chrome.tabs.query({active:true,currentWindow:true});
    currentSubreddit=globalThis.__rssPalReddit.detectSubreddit(tab?.url);
    document.getElementById('redditCurrent').style.display=currentSubreddit?'block':'none';
    if (currentSubreddit) {
      document.getElementById('redditCurrentName').textContent='当前 subreddit：r/'+currentSubreddit;
      const exists=boards.includes(currentSubreddit);
      add.disabled=exists;add.textContent=exists?'已加入探索':'加入探索';
    }
  }
  const labels={collecting:'采集中',failed:'采集失败',upload_failed:'上传失败',done:'已上传'};
  async function render() {
    await renderCurrent();
    const data=await chrome.storage.local.get(['redditDiscovery','redditDiscoveryEnabled']);
    toggle.checked=data.redditDiscoveryEnabled===true;
    const s=data.redditDiscovery;
    if (!s) return;
    const lines=['需要管理员 Token；每分钟处理一个榜单。'];
    if (s.error) lines.push(s.error);
    for (const [key,j] of Object.entries(s.jobs || {})) {
      lines.push(key.replace(':',' / ')+'：'+(j.requested?'待采集':labels[j.status] || '等待')+
        (j.status==='done'?'，达标 '+(j.qualified || 0)+'，候选入队 '+(j.accepted || 0)+(j.duplicate?'（重复批次）':''):'')+
        (j.error?' · '+j.error:''));
    }
    status.style.whiteSpace='pre-line';status.textContent=lines.join('\n');
  }
  async function send(message) {
    const response=await chrome.runtime.sendMessage(message);
    if (!response?.ok) throw new Error(response?.error || '无法启动探索');
    return response;
  }
  add.addEventListener('click',async()=>{
    if (!currentSubreddit) return;
    add.disabled=true;addStatus.textContent='正在加入…';
    try {await send({action:'redditDiscoveryAdd',subreddit:currentSubreddit});addStatus.textContent='已加入探索，首次采集已排队。';}
    catch(e) {addStatus.textContent=e.message;}
    finally {await renderCurrent();}
  });
  toggle.addEventListener('change',async()=>{
    try {await send({action:'redditDiscoveryConfigure',enabled:toggle.checked});}
    catch(e) {status.textContent=e.message;}
  });
  button.addEventListener('click',async()=>{
    button.disabled=true;
    try {await send({action:'redditDiscoveryRun'});status.textContent='已加入探索队列，每分钟处理一个榜单。';}
    catch(e) {status.textContent=e.message;}
    finally {button.disabled=false;}
  });
  chrome.storage.onChanged.addListener((_changes,area)=>{if(area==='local' || area==='sync')render();});
  render();
  send({action:'redditDiscoverySync'}).then(()=>render()).catch(e=>{status.textContent=e.message;});
})();
