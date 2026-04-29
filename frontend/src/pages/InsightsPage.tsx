import { useState, useEffect } from 'react'
import { getTopics, InterestTopic } from '../api/client'

export default function InsightsPage() {
  const [topics, setTopics] = useState<InterestTopic[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    loadTopics()
  }, [])

  const loadTopics = async () => {
    try {
      const data = await getTopics()
      setTopics(data || [])
    } finally {
      setLoading(false)
    }
  }

  if (loading) return <div className="card">Loading...</div>

  return (
    <div>
      <h2>兴趣洞察</h2>

      <div className="card">
        <h3 className="mb-2">兴趣主题</h3>
        {topics.length === 0 ? (
          <div className="text-muted">暂无数据，阅读文章并标记喜欢后将生成兴趣主题</div>
        ) : (
          <div className="flex gap-1" style={{ flexWrap: 'wrap' }}>
            {topics.map(topic => (
              <span
                key={topic.id}
                className="card"
                style={{
                  padding: '4px 12px',
                  fontSize: Math.min(16 + topic.weight * 2, 24),
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
        <h3 className="mb-2">使用建议</h3>
        <ul style={{ paddingLeft: 20 }}>
          <li>标记喜欢的文章会提升相关主题的权重</li>
          <li>标记不喜欢的文章会降低相关主题的权重</li>
          <li>保存文章表示你对这个主题特别感兴趣</li>
          <li>阅读时长也会影响推荐算法</li>
          <li>系统会根据你的偏好自动推荐相关文章</li>
        </ul>
      </div>
    </div>
  )
}
