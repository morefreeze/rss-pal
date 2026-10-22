import { Link } from 'react-router-dom'

interface Props {
 article: { parent_article_id?: number | null; parent_title?: string }
}

export default function ArticleProvenance({ article }: Props) {
 if (article.parent_article_id == null) return null
 return <div className="text-muted text-sm" style={{ margin: '6px 0', overflowWrap: 'anywhere' }}>
  <span>来自这篇文章：</span>
  <Link to={`/articles/${article.parent_article_id}`} onClick={event => event.stopPropagation()}>
   {article.parent_title || '查看原文章'}
  </Link>
 </div>
}
