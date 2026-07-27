import { resetArticleDetailCache } from './articleDetailCache'
import { logout } from './client'

export function clearPrivateSessionState(): void {
  resetArticleDetailCache()
  logout()
}
