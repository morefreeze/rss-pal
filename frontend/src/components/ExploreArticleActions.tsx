import type { useExploreArticleActions } from '../hooks/useExploreArticleActions'
export default function ExploreArticleActions({ actions }: { actions: ReturnType<typeof useExploreArticleActions> }) {
  return <div className="explore-article-actions">
    <button type="button" className="btn-ghost btn-sm" disabled={actions.pending || actions.skipped} onClick={() => void actions.skip()}>{actions.skipped ? '已略过' : '略过这篇'}</button>
    <button type="button" className="btn-ghost btn-sm" aria-pressed={actions.saved} disabled={actions.pending} onClick={() => void actions.toggleSaved()}>{actions.saved ? '取消稍后读' : '稍后读'}</button>
  </div>
}
