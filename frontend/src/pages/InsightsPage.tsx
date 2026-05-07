import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import {
  getTopics, getTags, getLatestInsights, generateInsightsStream,
  deleteTopic, deleteTag,
  InterestTopic, InterestTag, PersistedInsight,
} from '../api/client'

type Phase = 'loading' | 'empty' | 'has' | 'streaming'

export default function InsightsPage() {
  const navigate = useNavigate()
  const [phase, setPhase] = useState<Phase>('loading')
  const [topics, setTopics] = useState<InterestTopic[]>([])
  const [tags, setTags] = useState<InterestTag[]>([])
  const [insight, setInsight] = useState<PersistedInsight | null>(null)
  const [streamText, setStreamText] = useState('')
  const [remainingToday, setRemainingToday] = useState(3)
  const [remainingMonth, setRemainingMonth] = useState(100)
  const [errorMsg, setErrorMsg] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

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
      }
      const empty = t.length === 0 && g.length === 0 && (!latest || !latest.insight)
      setPhase(empty ? 'empty' : 'has')
    })
    return () => abortRef.current?.abort()
  }, [])

  const handleGenerate = () => {
    if (remainingToday <= 0) return
    setPhase('streaming')
    setStreamText('')
    setErrorMsg(null)
    abortRef.current = new AbortController()
    generateInsightsStream({
      onDelta: t => setStreamText(prev => prev + t),
      onDone: (full, quota) => {
        setInsight({
          id: 0,
          content: full,
          triggered_by: 'manual',
          generated_at: new Date().toISOString(),
        })
        setStreamText('')
        setRemainingToday(quota.remaining_today)
        setRemainingMonth(quota.remaining_month)
        setPhase('has')
      },
      onError: (msg, quota) => {
        setErrorMsg(msg === 'quota_exceeded' ? '今日已达上限' :
                    msg === 'no_data' ? '暂无足够数据' : `生成失败：${msg}`)
        if (quota) {
          setRemainingToday(quota.remaining_today)
          setRemainingMonth(quota.remaining_month)
        }
        setStreamText('')
        setPhase(insight ? 'has' : 'empty')
      },
    }, abortRef.current.signal)
  }

  const handleDeleteTopic = async (id: number) => {
    setTopics(prev => prev.filter(t => t.id !== id))
    try { await deleteTopic(id) }
    catch { /* keep optimistic UI; refresh will reconcile */ }
  }
  const handleDeleteTag = async (id: number) => {
    setTags(prev => prev.filter(t => t.id !== id))
    try { await deleteTag(id) }
    catch { /* keep optimistic UI */ }
  }

  if (phase === 'loading') return <div className="card">Loading...</div>
  if (phase === 'empty') return <EmptyState onGo={() => navigate('/articles')} />

  const buttonLabel = phase === 'streaming' ? '分析中...' :
                      remainingToday <= 0 ? '今日已达上限' :
                      `重新生成 (今日 ${3 - remainingToday}/3)`
  const subtitle = insight ? formatSubtitle(insight) : ''

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
            disabled={phase === 'streaming' || remainingToday <= 0}
            title={`今日剩 ${remainingToday} 次 · 本月剩 ${remainingMonth} 次`}
            style={{ fontSize: 13, padding: '4px 12px' }}
          >
            {buttonLabel}
          </button>
        </div>
        {subtitle && <div className="text-muted text-sm mb-1">{subtitle}</div>}
        {errorMsg && <div className="text-muted text-sm mb-1" style={{ color: '#c0392b' }}>{errorMsg}</div>}

        {phase === 'streaming' ? (
          <div className="markdown-body">
            <ReactMarkdown>{streamText || '正在分析…'}</ReactMarkdown>
          </div>
        ) : insight ? (
          <div className="markdown-body">
            <ReactMarkdown>{insight.content}</ReactMarkdown>
          </div>
        ) : (
          <div className="text-muted text-sm">点击右上角生成洞察</div>
        )}
      </div>

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
