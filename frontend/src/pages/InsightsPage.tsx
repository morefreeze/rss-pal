import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import {
  getTopics, getTags, getLatestInsights, generateInsights,
  deleteTopic, deleteInterestTag,
  InterestTopic, InterestTag, PersistedInsight, RecArticleMeta,
} from '../api/client'
import RecommendationsCard from '../components/RecommendationsCard'

type Phase = 'loading' | 'empty' | 'has'

const POLL_MS = 2000

export default function InsightsPage() {
  const navigate = useNavigate()
  const [phase, setPhase] = useState<Phase>('loading')
  const [topics, setTopics] = useState<InterestTopic[]>([])
  const [tags, setTags] = useState<InterestTag[]>([])
  const [insight, setInsight] = useState<PersistedInsight | null>(null)
  const [recArticles, setRecArticles] = useState<Record<string, RecArticleMeta>>({})
  const [remainingToday, setRemainingToday] = useState(3)
  const [remainingMonth, setRemainingMonth] = useState(100)
  const [errorMsg, setErrorMsg] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const pollRef = useRef<number | null>(null)

  const refresh = async () => {
    const latest = await getLatestInsights().catch(() => null)
    if (!latest) return null
    setRemainingToday(latest.remaining_today)
    setRemainingMonth(latest.remaining_month)
    setInsight(latest.insight)
    setRecArticles(latest.rec_articles || {})
    return latest.insight
  }

  useEffect(() => {
    Promise.all([
      getTopics().then(d => d || []).catch(() => []),
      getTags().then(d => d || []).catch(() => []),
      getLatestInsights().catch(() => null),
    ]).then(([t, g, latest]) => {
      setTopics(t)
      setTags(g)
      if (latest) {
        setRemainingToday(latest.remaining_today)
        setRemainingMonth(latest.remaining_month)
        setInsight(latest.insight)
        setRecArticles(latest.rec_articles || {})
      }
      const empty = t.length === 0 && g.length === 0 && (!latest || !latest.insight)
      setPhase(empty ? 'empty' : 'has')
    })
    return () => { if (pollRef.current) window.clearInterval(pollRef.current) }
  }, [])

  // Auto-poll while a pending row exists.
  useEffect(() => {
    if (insight?.status !== 'pending') {
      if (pollRef.current) { window.clearInterval(pollRef.current); pollRef.current = null }
      return
    }
    if (pollRef.current) return
    pollRef.current = window.setInterval(refresh, POLL_MS)
    return () => { if (pollRef.current) { window.clearInterval(pollRef.current); pollRef.current = null } }
  }, [insight?.status])

  const handleGenerate = async () => {
    if (busy || remainingToday <= 0 || insight?.status === 'pending') return
    setBusy(true)
    setErrorMsg(null)
    try {
      const resp = await generateInsights()
      if (resp.status === 'no_data') {
        setErrorMsg(resp.message || '暂无足够数据')
        return
      }
      // status === 'pending': server kicked off the job; refresh to pick up the pending row.
      setRemainingToday(resp.remaining_today)
      setRemainingMonth(resp.remaining_month)
      await refresh()
    } catch (e: any) {
      const status = e?.response?.status
      if (status === 429) setErrorMsg('今日已达上限')
      else if (status === 409) setErrorMsg('已有生成任务在进行中')
      else setErrorMsg(`生成失败：${e?.message || '请稍后重试'}`)
    } finally {
      setBusy(false)
    }
  }

  const handleDeleteTopic = async (id: number) => {
    setTopics(prev => prev.filter(t => t.id !== id))
    try { await deleteTopic(id) }
    catch { /* keep optimistic UI */ }
  }
  const handleDeleteTag = async (id: number) => {
    setTags(prev => prev.filter(t => t.id !== id))
    try { await deleteInterestTag(id) }
    catch { /* keep optimistic UI */ }
  }

  if (phase === 'loading') return <div className="card">Loading...</div>
  if (phase === 'empty') return <EmptyState onGo={() => navigate('/articles')} />

  const isPending = insight?.status === 'pending'
  const buttonLabel = busy ? '提交中…' :
                      isPending ? '生成中…' :
                      remainingToday <= 0 ? '今日已达上限' :
                      `重新生成 (今日 ${remainingToday}/3)`
  const buttonDisabled = busy || isPending || remainingToday <= 0
  const subtitle = insight && insight.status === 'done' ? formatSubtitle(insight) : ''

  return (
    <div>
      <h2 className="mb-2">兴趣洞察</h2>

      <Cloud
        title="兴趣主题"
        size="lg"
        empty="暂无主题，多阅读并标记后将出现"
        items={topics.map(t => ({ id: t.id, label: t.topic, weight: t.weight }))}
        onDelete={handleDeleteTopic}
      />

      <Cloud
        title="关键词"
        size="sm"
        empty="暂无关键词"
        items={tags.slice(0, 30).map(t => ({ id: t.id, label: t.tag, weight: t.weight }))}
        onDelete={handleDeleteTag}
      />

      <div className="card">
        <div className="flex-between mb-2">
          <h3>AI 个性化洞察</h3>
          <button
            onClick={handleGenerate}
            disabled={buttonDisabled}
            title={`今日剩 ${remainingToday} 次 · 本月剩 ${remainingMonth} 次`}
            style={{ fontSize: 13, padding: '4px 12px' }}
          >
            {buttonLabel}
          </button>
        </div>
        {subtitle && <div className="text-muted text-sm mb-1">{subtitle}</div>}
        {errorMsg && <div className="text-muted text-sm mb-1" style={{ color: '#c0392b' }}>{errorMsg}</div>}

        {isPending ? (
          <div className="text-muted text-sm" style={{ padding: '12px 0' }}>
            🌀 正在生成中，需要 30 秒到 1 分钟，稍后回来查看（页面会自动刷新）
          </div>
        ) : insight?.status === 'failed' ? (
          <div className="text-sm" style={{ color: '#c0392b' }}>
            上次生成失败：{insight.error_msg || '未知错误'}（不消耗配额，可重试）
          </div>
        ) : insight?.status === 'done' && insight.content ? (
          <div className="markdown-body">
            <ReactMarkdown>{insight.content}</ReactMarkdown>
          </div>
        ) : (
          <div className="text-muted text-sm">点击右上角生成洞察</div>
        )}
      </div>

      {insight?.status === 'done' && insight.recommendations && (
        <RecommendationsCard
          recommendations={insight.recommendations}
          articles={recArticles}
        />
      )}

      <div className="card">
        <h3 className="mb-2">提升推荐质量</h3>
        <ul style={{ paddingLeft: 20, lineHeight: 2 }}>
          <li>标记喜欢的文章会提升相关主题的权重</li>
          <li>标记不喜欢的文章会降低相关主题的权重</li>
          <li>保存文章表示你对这个主题特别感兴趣</li>
          <li>阅读时长也会影响推荐算法</li>
        </ul>
      </div>
    </div>
  )
}

function formatSubtitle(ins: PersistedInsight): string {
  const ago = formatAgo(ins.generated_at)
  return ins.triggered_by === 'auto'
    ? `${ago} · 由系统自动生成`
    : `${ago} · 你触发的`
}

function formatAgo(iso: string): string {
  const t = new Date(iso).getTime()
  const dm = (Date.now() - t) / 60000
  if (dm < 1) return '刚刚'
  if (dm < 60) return `${Math.floor(dm)} 分钟前`
  const h = dm / 60
  if (h < 24) return `${Math.floor(h)} 小时前`
  return `${Math.floor(h / 24)} 天前`
}

function EmptyState({ onGo }: { onGo: () => void }) {
  return (
    <div className="card" style={{ textAlign: 'center', padding: 40 }}>
      <h3>💡 还没有足够数据生成洞察</h3>
      <p className="text-muted">洞察基于你对文章的反应生成。试着：</p>
      <ul style={{ display: 'inline-block', textAlign: 'left', lineHeight: 2 }}>
        <li>多阅读一会文章</li>
        <li>给文章点个 ❤️</li>
        <li>收藏感兴趣的文章</li>
      </ul>
      <div style={{ marginTop: 16 }}>
        <button onClick={onGo}>去阅读文章 →</button>
      </div>
    </div>
  )
}

function Cloud({ title, size, items, empty, onDelete }: {
  title: string
  size: 'lg' | 'sm'
  items: { id: number; label: string; weight: number }[]
  empty: string
  onDelete: (id: number) => void
}) {
  const [hover, setHover] = useState<number | null>(null)
  const baseSize = size === 'lg' ? 14 : 11
  const grow = size === 'lg' ? 2 : 1

  return (
    <div className="card">
      <h3 className="mb-2">{title}</h3>
      {items.length === 0 ? (
        <div className="text-muted">{empty}</div>
      ) : (
        <div className="flex gap-1" style={{ flexWrap: 'wrap' }}>
          {items.map(it => (
            <span
              key={it.id}
              onMouseEnter={() => setHover(it.id)}
              onMouseLeave={() => setHover(null)}
              style={{
                position: 'relative',
                padding: '4px 12px',
                paddingRight: hover === it.id ? 28 : 12,
                background: '#e8f0fe',
                borderRadius: 20,
                color: '#1a56db',
                fontSize: Math.min(baseSize + it.weight * grow, size === 'lg' ? 24 : 16),
                fontWeight: it.weight > 3 ? 600 : 400,
                transition: 'padding 0.1s',
              }}
            >
              {it.label}
              {hover === it.id && (
                <button
                  onClick={() => onDelete(it.id)}
                  style={{
                    position: 'absolute', right: 4, top: '50%', transform: 'translateY(-50%)',
                    width: 18, height: 18, borderRadius: '50%', border: 'none',
                    background: '#c0392b', color: 'white', cursor: 'pointer',
                    fontSize: 11, lineHeight: 1, padding: 0,
                  }}
                  aria-label="删除"
                >×</button>
              )}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
