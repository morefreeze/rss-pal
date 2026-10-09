(function () {
  'use strict';
  const toggle=document.getElementById('redditAuto');
  const button=document.getElementById('redditRun');
  const status=document.getElementById('redditStatus');
  const labels={collecting:'采集中',failed:'采集失败',upload_failed:'上传失败',done:'已上传'};
  async function render() {
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
    try {const response=await chrome.runtime.sendMessage(message);if (!response?.ok) throw new Error('无法启动探索');}
    catch(e) {status.textContent=e.message;}
  }
  toggle.addEventListener('change',()=>send({action:'redditDiscoveryConfigure',enabled:toggle.checked}));
  button.addEventListener('click',async()=>{
    button.disabled=true;await send({action:'redditDiscoveryRun'});status.textContent='已加入探索队列，每分钟处理一个榜单。';button.disabled=false;
  });
  chrome.storage.onChanged.addListener((_changes,area)=>{if(area==='local')render();});
  render();
})();
