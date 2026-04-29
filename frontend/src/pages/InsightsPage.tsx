import { useState, useEffect } from 'react'
import ReactMarkdown from 'react-markdown'
import { getTopics, generateInsights, InterestTopic } from '../api/client'

export default function InsightsPage() {
  const [topics, setTopics] = useState<InterestTopic[]>([])
  const [insights, setInsights] = useState('')
  const [insightsMessage, setInsightsMessage] = useState('')
  const [loading, setLoading] = useState(true)
  const [generating, setGenerating] = useState(false)

  useEffect(() => {
    getTopics()
      .then(data => setTopics(data || []))
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const handleGenerateInsights = async () => {
    setGenerating(true)
    setInsights('')
    setInsightsMessage('')
    try {
      const result = await generateInsights()
      if (result.insights) {
        setInsights(result.insights)
      } else if (result.message) {
        setInsightsMessage(result.message)
      }
    } catch {
      setInsightsMessage('生成失败，请稍后重试')
    } finally {
      setGenerating(false)
    }
  }

  if (loading) return <div className="card">Loading...</div>

  return (
    <div>
      <h2 className="mb-2">兴趣洞察</h2>

      <div className="card">
        <div className="flex-between mb-2">
          <h3>兴趣主题</h3>
        </div>
        {topics.length === 0 ? (
          <div className="text-muted">暂无数据，阅读文章并标记喜欢后将生成兴趣主题</div>
        ) : (
          <div className="flex gap-1" style={{ flexWrap: 'wrap' }}>
            {topics.map(topic => (
              <span
                key={topic.id}
                style={{
                  display: 'inline-block',
                  padding: '4px 12px',
                  background: '#e8f0fe',
                  borderRadius: 20,
                  color: '#1a56db',
                  fontSize: Math.min(12 + topic.weight * 1.5, 20),
                  fontWeight: topic.weight > 3 ? 600 : 400,
                }}
              >
                {topic.topic}
              </span>
            ))}
          </div>
        )}
      </div>

      <div className="card">
        <div className="flex-between mb-2">
          <h3>AI 个性化洞察</h3>
          <button
            onClick={handleGenerateInsights}
            disabled={generating}
            style={{ fontSize: 13, padding: '4px 12px' }}
          >
            {generating ? '分析中...' : insights ? '重新分析' : '生成洞察'}
          </button>
        </div>
        {generating && (
          <div className="text-muted text-sm">正在分析你的阅读习惯，请稍候...</div>
        )}
        {insights && (
          <div className="markdown-body">
            <ReactMarkdown>{insights}</ReactMarkdown>
          </div>
        )}
        {insightsMessage && !insights && !generating && (
          <div className="text-muted text-sm">{insightsMessage}</div>
        )}
        {!insights && !insightsMessage && !generating && (
          <div className="text-muted text-sm">点击右上角"生成洞察"，AI 将基于你的阅读偏好分析兴趣趋势</div>
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
