import { useState } from 'react'
import { Article, GroupedArticles, TopicGroup } from '../api/client'
import ArticleCard from './ArticleCard'

const INITIAL_PER_GROUP = 5

interface Props {
  data: GroupedArticles
  isRead: (a: Article) => boolean
  formatDate: (d: string | null) => string
  stripMarkdown: (t: string) => string
  onOpen: (id: number) => void
  onPlay: (a: Article) => void
}

function GroupSection({
  group,
  label,
  isRead, formatDate, stripMarkdown, onOpen, onPlay,
}: {
  group: TopicGroup
  label: string
} & Pick<Props, 'isRead' | 'formatDate' | 'stripMarkdown' | 'onOpen' | 'onPlay'>) {
  const [expanded, setExpanded] = useState(false)
  if (group.articles.length === 0) return null

  const visible = expanded ? group.articles : group.articles.slice(0, INITIAL_PER_GROUP)
  const hiddenInResponse = group.articles.length - visible.length
  const beyondResponse = group.total_count - group.articles.length

  return (
    <section style={{ marginBottom: 24 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, padding: '8px 4px', borderBottom: '1px solid #eee', marginBottom: 8 }}>
        <h3 style={{ margin: 0, fontSize: 16 }}>{label}</h3>
        <span className="text-muted text-sm">· {group.total_count} 篇</span>
      </div>
      {visible.map((article, idx) => (
        <ArticleCard
          key={article.id}
          article={article}
          manualTags={[]}
          isRead={isRead(article)}
          isFocused={false}
          idx={idx}
          onPlay={onPlay}
          formatDate={formatDate}
          stripMarkdown={stripMarkdown}
          onOpen={onOpen}
          onFocus={() => {}}
        />
      ))}
      {(hiddenInResponse > 0 || (!expanded && beyondResponse > 0)) && (
        <div style={{ textAlign: 'center', padding: 8 }}>
          <button
            type="button"
            className="secondary"
            style={{ fontSize: 13, padding: '6px 14px' }}
            onClick={() => setExpanded(true)}
          >
            展开更多 ({hiddenInResponse + Math.max(0, beyondResponse)})
          </button>
        </div>
      )}
    </section>
  )
}

export default function GroupedArticleView({ data, isRead, formatDate, stripMarkdown, onOpen, onPlay }: Props) {
  const hasAny = data.groups.length > 0 || data.unclassified.articles.length > 0
  if (!hasAny) {
    return <div className="card text-muted">暂无文章。</div>
  }
  return (
    <div>
      {data.groups.map(g => (
        <GroupSection
          key={g.topic}
          group={g}
          label={g.topic}
          isRead={isRead}
          formatDate={formatDate}
          stripMarkdown={stripMarkdown}
          onOpen={onOpen}
          onPlay={onPlay}
        />
      ))}
      {data.unclassified.articles.length > 0 && (
        <GroupSection
          group={data.unclassified}
          label="未分类"
          isRead={isRead}
          formatDate={formatDate}
          stripMarkdown={stripMarkdown}
          onOpen={onOpen}
          onPlay={onPlay}
        />
      )}
    </div>
  )
}
