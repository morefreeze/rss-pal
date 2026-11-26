import { resetArticleDetailCache } from './articleDetailCache'
import { logout } from './client'

export function clearPrivateSessionState(): Promise<void> {
  resetArticleDetailCache()
  return logout()
}
