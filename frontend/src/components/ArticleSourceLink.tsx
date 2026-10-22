import type { CSSProperties } from 'react'
import type { Article, ArticleListItem } from '../api/client'
import FeedSourceLink from './FeedSourceLink'

export type SourceArticle = Pick<Article | ArticleListItem, 'feed_id' | 'feed_title' | 'url' | 'source'>

interface Props {
 article: SourceArticle
 label?: string
 search?: string
 className?: string
 style?: CSSProperties
 onNavigate?: (feedId: number, href: string) => void
}

// Publication source and subscription navigation are separate: an extracted
// link opens its original website; a feed article retains feed filtering.
export default function ArticleSourceLink({ article, label, search, className = 'tag-chip tag-chip-source', style, onNavigate }: Props) {
 if (article.source) {
  return <a href={article.source.url} target="_blank" rel="noopener noreferrer" className={className}
   style={{ textDecoration: 'none', ...style }} aria-label={`原文来源：${article.source.title}`}
   onClick={event => event.stopPropagation()}>{article.source.title}</a>
 }
 const title = label ?? article.feed_title
 if (!title) return null
 return <FeedSourceLink feedId={article.feed_id} label={title} search={search} className={className} style={style} onNavigate={onNavigate} />
}
