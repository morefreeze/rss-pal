import { useLocation } from 'react-router-dom'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { getRoutePageTitle, type ArticleTitleLocationState } from '../utils/pageTitle'
import { isPreviewPath } from '../workspace/workspaceRoutes'

export default function RoutePageTitle({ allowPreviews = true }: { allowPreviews?: boolean }) {
  const location = useLocation()
  const pageTitle = !allowPreviews && isPreviewPath(location.pathname) ? '文章列表' : getRoutePageTitle(
    location.pathname,
    location.search,
    location.state as ArticleTitleLocationState | null,
  )
  useDocumentTitle(pageTitle)
  return null
}
