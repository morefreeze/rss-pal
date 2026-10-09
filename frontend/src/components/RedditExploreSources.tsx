import { useState } from 'react'
import { getRedditSubreddits, removeRedditSubreddit, getUser, type RedditSubreddit } from '../api/client'

export default function RedditExploreSources() {
 const [open,setOpen]=useState(false)
 const [items,setItems]=useState<RedditSubreddit[]>([])
 const [loading,setLoading]=useState(false)
 const [busy,setBusy]=useState<string|null>(null)
 const [error,setError]=useState('')
 if (!getUser()?.is_admin) return null
 async function load() {
  setLoading(true);setError('')
  try {setItems((await getRedditSubreddits()).filter(item=>item.enabled))}
  catch {setError('无法加载 subreddit 列表，请重试')}
  finally {setLoading(false)}
 }
 async function remove(name:string) {
  setBusy(name);setError('')
  try {await removeRedditSubreddit(name);setItems(current=>current.filter(item=>item.name!==name))}
  catch {setError('移除失败，请重试')}
  finally {setBusy(null)}
 }
 return <section className="card" style={{marginBottom:16}} aria-label="Reddit 探索来源">
  <button type="button" className="btn-ghost" aria-expanded={open} onClick={()=>{setOpen(!open);if(!open)void load()}}>管理 subreddit</button>
  {open && <div>
   <p className="text-muted">由浏览器插件采集周榜和月榜，至少 100 分。移除将停止后续采集，已发现的文章保留。添加请在 Reddit 页面打开插件。</p>
   <button type="button" className="secondary" disabled={loading||busy!==null} onClick={()=>void load()}>刷新列表</button>
   {error && <p role="alert">{error}</p>}
   {loading ? <p>加载中…</p> : items.length===0 ? <p>尚未添加 subreddit</p> : <ul style={{listStyle:'none',padding:0}}>{items.map(item=><li key={item.name} style={{display:'flex',alignItems:'center',gap:12,padding:'8px 0'}}>
    <a href={`https://www.reddit.com/r/${item.name}/`} target="_blank" rel="noreferrer">r/{item.name}</a>
    <span className="text-muted" style={{flex:1}}>{item.last_success_at ? `最近采集 ${new Date(item.last_success_at).toLocaleString('zh-CN')}` : '尚未成功采集'}</span>
    <button type="button" className="secondary" aria-label={`移除 r/${item.name}`} disabled={busy!==null} onClick={()=>void remove(item.name)}>{busy===item.name?'移除中…':'移除'}</button>
   </li>)}</ul>}
  </div>}
 </section>
}
