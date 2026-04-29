import { useState, useEffect } from 'react'
import { useParams } from 'react-router-dom'
import axios from 'axios'
import ReactMarkdown from 'react-markdown'

interface SharedArticle {
  id: number
  title: string
  url: string
  summary_brief: string
  summary_detailed: string
  published_at: string | null
}

export default function SharePage() {
  const { token } = useParams<{ token: string }>()
  const [article, setArticle] = useState<SharedArticle | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!token) return
    axios.get<SharedArticle>('/api/share/' + token)
      .then(res => setArticle(res.data))
      .catch(() => setError('分享链接无效或已过期'))
      .finally(() => setLoading(false))
  }, [token])

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return ''
    return new Date(dateStr).toLocaleString('zh-CN')
  }

  if (loading) {
    return (
      <div style={{ maxWidth: 720, margin: '40px auto', padding: '0 16px' }}>
        <div className="card">加载中...</div>
      </div>
    )
  }

  if (error || !article) {
    return (
      <div style={{ maxWidth: 720, margin: '40px auto', padding: '0 16px' }}>
        <div className="card" style={{ textAlign: 'center' }}>
          <p style={{ color: '#ef4444', marginBottom: 16 }}>{error || '文章不存在'}</p>
          <a href="/" style={{ color: '#0066cc' }}>返回 RSS Pal 首页</a>
        </div>
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 720, margin: '40px auto', padding: '0 16px' }}>
      {/* Header branding */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 20 }}>
        <span style={{ fontWeight: 700, fontSize: 18, color: '#0066cc' }}>RSS Pal</span>
        <span style={{ color: '#999', fontSize: 14 }}>· 分享文章</span>
      </div>

      {/* Article title card */}
      <div className="card">
        <h2 style={{ marginBottom: 8 }}>{article.title}</h2>
        {article.published_at && (
          <div className="text-muted text-sm mb-2">{formatDate(article.published_at)}</div>
        )}
        <a
          href={article.url}
          target="_blank"
          rel="noopener noreferrer"
          style={{ fontSize: 14, color: '#0066cc', wordBreak: 'break-all' }}
        >
          {article.url}
        </a>
      </div>

      {/* Summary */}
      {(article.summary_brief || article.summary_detailed) && (
        <div className="card">
          <h3 style={{ marginBottom: 10 }}>AI 总结</h3>
          <div className="markdown-body">
            {article.summary_brief && <ReactMarkdown>{article.summary_brief}</ReactMarkdown>}
            {article.summary_brief && article.summary_detailed && (
              <hr style={{ margin: '12px 0', borderColor: '#eee' }} />
            )}
            {article.summary_detailed && <ReactMarkdown>{article.summary_detailed}</ReactMarkdown>}
          </div>
        </div>
      )}

      {/* Original link button */}
      <div className="card" style={{ textAlign: 'center' }}>
        <a
          href={article.url}
          target="_blank"
          rel="noopener noreferrer"
        >
          <button style={{ fontSize: 15, padding: '8px 24px' }}>
            阅读原文
          </button>
        </a>
      </div>

      {/* Footer watermark */}
      <div style={{ textAlign: 'center', marginTop: 32, marginBottom: 24, color: '#aaa', fontSize: 13 }}>
        <span>由 </span>
        <a href="/" style={{ color: '#0066cc', fontWeight: 600 }}>RSS Pal</a>
        <span> 提供 · </span>
        <a href="/" style={{ color: '#aaa' }}>在 RSS Pal 中阅读更多</a>
      </div>
    </div>
  )
}
