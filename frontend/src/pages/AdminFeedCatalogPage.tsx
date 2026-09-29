import { useEffect, useRef, useState, type FormEvent } from 'react'
import { listAdminFeedCatalog, createCatalogItem, updateCatalogItem, checkCatalogItem, publishCatalogItem, type CatalogFields, type CatalogItem } from '../api/feedCatalog'
import './AdminFeedCatalogPage.css'

const blank: CatalogFields = { title: '', url: '', category: '', description: '', sort_order: 0 }
const checkLabels = { unchecked: '尚未检查', ok: '检查通过', failed: '检查失败' }
function errorMessage(error: unknown) {
  const e = error as { response?: { status?: number; data?: { error?: string } } }
  if (e.response?.status === 409) return '条目已被修改，请刷新列表后重新编辑或操作。'
  return e.response?.data?.error || '操作失败，请稍后重试。'
}
export default function AdminFeedCatalogPage({ user }: { user?: { is_admin: boolean } | null }) {
  const [items, setItems] = useState<CatalogItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const [editor, setEditor] = useState<{ id?: number; revision?: number; fields: CatalogFields } | null>(null)
  const [busy, setBusy] = useState(false)
  const locked = useRef(false)
  const isAdmin = !!user?.is_admin
  const load = async () => {
    setLoading(true); setError('')
    try { setItems(await listAdminFeedCatalog() || []) }
    catch (e) { setError(errorMessage(e)) }
    finally { setLoading(false) }
  }
  useEffect(() => { if (isAdmin) void load() }, [isAdmin])
  const mutate = async (action: () => Promise<CatalogItem>, success: string, closeEditor = false) => {
    if (locked.current) return
    locked.current = true; setBusy(true); setError(''); setNotice('')
    try {
      const result = await action()
      setItems(current => [...current.filter(i => i.id !== result.id), result])
      setNotice(result.check_status === 'failed' ? '检查失败，请查看条目中的原因后重试。' : success)
      if (closeEditor) setEditor(null)
    } catch (e) {
      setError(errorMessage(e))
      // A failed publication records its check and advances the server revision.
      // Refresh that result so the next retry does not replay a stale revision.
      if ((e as { response?: { status?: number } }).response?.status === 422) {
        try { setItems(await listAdminFeedCatalog() || []) }
        catch { setError(`${errorMessage(e)} 请刷新列表后重试。`) }
      }
    }
    finally { locked.current = false; setBusy(false) }
  }
  const save = (event: FormEvent) => {
    event.preventDefault()
    if (!editor) return
    const { id, revision, fields } = editor
    void mutate(() => id === undefined ? createCatalogItem(fields) : updateCatalogItem(id, { ...fields, revision: revision! }), '已保存', true)
  }
  if (!isAdmin) return <div className="card">仅管理员可访问公共推荐目录</div>
  const visible = items.filter(i => `${i.title} ${i.url}`.toLowerCase().includes(search.trim().toLowerCase()) && (filter === 'all' || i.published === (filter === 'published'))).sort((a, b) => a.sort_order - b.sort_order || a.id - b.id)
  return <div className="feed-catalog-admin">
    <h2>公共推荐目录</h2>
    <p className="text-muted">维护所有用户可选择订阅的推荐来源。上架时会检查 RSS；修改地址后需重新上架。</p>
    <div className="catalog-controls">
      <button disabled={busy || loading} onClick={() => { setEditor({ fields: { ...blank } }); setNotice('') }}>新增条目</button>
      <button className="secondary" disabled={busy || loading} onClick={() => { setEditor(null); void load() }}>刷新列表</button>
      <input aria-label="搜索目录" placeholder="搜索标题或地址" value={search} onChange={e => setSearch(e.target.value)} />
      <select aria-label="发布状态" value={filter} onChange={e => setFilter(e.target.value)}><option value="all">全部状态</option><option value="draft">草稿</option><option value="published">已上架</option></select>
    </div>
    {error && <p role="alert" className="catalog-error">{error}</p>}
    {notice && <p role="status">{notice}</p>}
    {loading && <p role="status">正在加载目录…</p>}
    {busy && <p role="status">正在处理，请稍候…</p>}
    {editor && <form className="card catalog-editor" onSubmit={save}>
      <h3>{editor.id === undefined ? '新增草稿' : '编辑条目'}</h3>
      <fieldset disabled={busy}>
        {(['title', 'url', 'category', 'description'] as const).map(field => <label key={field}>
          {{ title: '标题', url: 'RSS 地址', category: '分类', description: '简介' }[field]}
          <input required={field !== 'description'} maxLength={{ title: 200, url: 2048, category: 80, description: 1000 }[field]} value={editor.fields[field]} onChange={e => setEditor({ ...editor, fields: { ...editor.fields, [field]: e.target.value } })} />
        </label>)}
        <label>排序<input type="number" required min={0} max={1000000} step={1} value={editor.fields.sort_order} onChange={e => setEditor({ ...editor, fields: { ...editor.fields, sort_order: Number(e.target.value) } })} /></label>
        <div className="catalog-controls"><button type="submit">{editor.id === undefined ? '保存草稿' : '保存修改'}</button><button type="button" className="secondary" onClick={() => setEditor(null)}>取消</button></div>
      </fieldset>
    </form>}
    {!loading && !error && visible.length === 0 && <p>没有符合条件的条目</p>}
    {visible.map(item => <article className="card catalog-item" key={item.id}>
      <div className="catalog-controls"><h3>{item.title}</h3><span>{item.published ? '已上架' : '草稿'}</span></div>
      <p className="catalog-url">{item.url}</p>
      <p className="text-muted">{item.category || '未分类'} · 排序 {item.sort_order}</p>
      {item.description && <p>{item.description}</p>}
      <p>{checkLabels[item.check_status]} · 最近检查：{item.last_checked_at ? new Date(item.last_checked_at).toLocaleString('zh-CN') : '尚未检查'}</p>
      {item.last_error && <p className="catalog-error">{item.last_error}</p>}
      <div className="catalog-controls">
        <button className="secondary" disabled={busy || loading} onClick={() => setEditor({ id: item.id, revision: item.revision, fields: { title: item.title, url: item.url, category: item.category, description: item.description, sort_order: item.sort_order } })}>编辑</button>
        <button className="secondary" disabled={busy || loading || !!editor} onClick={() => void mutate(() => checkCatalogItem(item.id, item.revision), '检查完成')}>检查</button>
        <button disabled={busy || loading || !!editor} onClick={() => void mutate(() => publishCatalogItem(item.id, !item.published, item.revision), item.published ? '已下架' : '已上架')}>{item.published ? '下架' : '上架'}</button>
      </div>
    </article>)}
  </div>
}
