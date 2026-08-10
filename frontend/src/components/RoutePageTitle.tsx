import { useLocation } from 'react-router-dom'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { getRoutePageTitle, type ArticleTitleLocationState } from '../utils/pageTitle'

export default function RoutePageTitle() {
  const location = useLocation()
  const pageTitle = getRoutePageTitle(
    location.pathname,
    location.search,
    location.state as ArticleTitleLocationState | null,
  )
  useDocumentTitle(pageTitle)
  return null
}
