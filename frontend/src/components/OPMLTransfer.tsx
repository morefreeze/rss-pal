import { useRef, useState } from 'react'
import { addFeed, exportOPML, getFeeds } from '../api/client'
import { feedURLKey, MAX_OPML_BYTES, OPMLEntry, parseOPML } from '../utils/opml'

type Result = OPMLEntry & { status: 'success' | 'duplicate' | 'failed'; reason: string }

export default function OPMLTransfer({ onImported }: { onImported: () => void | Promise<void> }) {
  const input = useRef<HTMLInputElement>(null)
  const busy = useRef(false)
  const [importing, setImporting] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [error, setError] = useState('')
  const [results, setResults] = useState<Result[]>([])
  const [progress, setProgress] = useState({ done: 0, total: 0 })
  const [hasResults, setHasResults] = useState(false)

  const run = async (entries: OPMLEntry[], previous: Result[] = []) => {
    const existing = await getFeeds()
    const known = new Set<string>()
    for (const feed of existing || []) {
      try { known.add(feedURLKey(feed.url)) } catch { /* non-portable internal source */ }
    }
    const seen = new Set<string>()
    const next = [...previous]
    setProgress({ done: 0, total: entries.length })
    for (const entry of entries) {
      let result: Result
      if (entry.error) {
        result = { ...entry, status: 'failed', reason: entry.error }
      } else if (known.has(feedURLKey(entry.url)) || seen.has(feedURLKey(entry.url))) {
        result = { ...entry, status: 'duplicate', reason: '已存在或文件内重复' }
      } else {
        seen.add(feedURLKey(entry.url))
        try {
          await addFeed(entry.url, entry.feedType, false, entry.title)
          known.add(feedURLKey(entry.url))
          result = { ...entry, status: 'success', reason: '已添加' }
        } catch (err: any) {
          if (err?.response?.status === 409) {
            known.add(feedURLKey(entry.url))
            result = { ...entry, status: 'duplicate', reason: '已存在' }
          } else {
            result = { ...entry, status: 'failed', reason: err?.response?.data?.error || err?.message || '添加失败，请重试' }
          }
        }
      }
      next.push(result)
      setResults([...next])
      setProgress(p => ({ ...p, done: p.done + 1 }))
    }
    setHasResults(true)
    try { await onImported() } catch { setError('订阅已处理，但列表刷新失败，请刷新页面查看') }
  }

  const importFile = async (file?: File) => {
    if (!file || busy.current) return
    busy.current = true
    setImporting(true)
    setError('')
    setResults([])
    setHasResults(false)
    setProgress({ done: 0, total: 0 })
    try {
      if (file.size > MAX_OPML_BYTES) throw new Error('文件不能超过 5 MiB')
      await run(parseOPML(await file.text()))
    } catch (err: any) { setError(err?.message || 'OPML 导入失败') }
    finally { busy.current = false; setImporting(false) }
  }

  const retry = async () => {
    if (busy.current) return
    busy.current = true
    setImporting(true)
    setError('')
    const failed = results.filter(r => r.status === 'failed' && !r.error)
    try { await run(failed, results.filter(r => r.status !== 'failed' || r.error)) }
    catch (err: any) { setError(err?.message || '重试失败') }
    finally { busy.current = false; setImporting(false) }
  }

  const download = async () => {
    setExporting(true)
    setError('')
    try {
      const blob = await exportOPML()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'rss-pal-subscriptions.opml'
      document.body.appendChild(a)
      a.click()
      a.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch { setError('导出失败，请重试') }
    finally { setExporting(false) }
  }

  return <section aria-label="OPML 导入导出" className="card mb-2">
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
      <input ref={input} aria-label="导入 OPML 文件" type="file" accept=".opml,.xml,text/xml,application/xml" hidden disabled={importing}
        onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; void importFile(file) }} />
      <button className="secondary" disabled={importing} onClick={() => input.current?.click()}>{importing ? '导入中…' : '导入 OPML'}</button>
      <button className="secondary" disabled={exporting || importing} onClick={() => void download()}>{exporting ? '导出中…' : '导出 OPML'}</button>
      <span className="text-sm text-muted">迁移订阅列表；文件夹中的订阅会平铺导入。最多 5 MiB、5000 个订阅。</span>
    </div>
    {error && <p role="alert">{error}</p>}
    {importing && <div role="status"><progress aria-label="导入进度" max={progress.total || 1} value={progress.done} /> {progress.done} / {progress.total}</div>}
    {(hasResults || results.length > 0) && <div>
      <p role="status">成功 {results.filter(r => r.status === 'success').length} · 重复 {results.filter(r => r.status === 'duplicate').length} · 失败 {results.filter(r => r.status === 'failed').length}</p>
      {!importing && results.some(r => r.status === 'failed' && !r.error) && <button className="secondary" onClick={() => void retry()}>重试失败项</button>}
      <details><summary>查看导入明细</summary>
        <ul style={{ maxHeight: 280, overflow: 'auto', overflowWrap: 'anywhere' }}>{results.map((r, i) => <li key={i}>
          {r.status === 'success' ? '成功' : r.status === 'duplicate' ? '重复' : '失败'}：{r.title || r.url || '空地址'} — {r.reason}{r.title && <span>（{r.url}）</span>}
        </li>)}</ul>
      </details>
    </div>}
  </section>
}
