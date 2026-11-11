import { resolveWorkspaceRoute, safeListPath, type WorkspaceModule } from './workspaceRoutes'
export type WorkspaceDestinations = Partial<Record<WorkspaceModule, string>>
const key = (accountId: number) => `reading-workspace-destinations:${accountId}`

export function readWorkspaceDestinations(accountId: number | undefined): WorkspaceDestinations {
  if (accountId == null) return {}
  try {
    const parsed = JSON.parse(sessionStorage.getItem(key(accountId)) || '{}')
    const result: WorkspaceDestinations = {}
    for (const module of ['articles', 'clips', 'explore', 'briefing'] as const) {
      const path = safeListPath(parsed?.[module])
      if (!path) continue
      const [pathname, query] = path.split('?')
      if (resolveWorkspaceRoute(pathname, query ? `?${query}` : '')?.module === module) result[module] = path
    }
    return result
  } catch { return {} }
}
export function rememberWorkspaceDestination(accountId: number | undefined, module: WorkspaceModule, path: string) {
  if (accountId == null || !safeListPath(path)) return
  try { sessionStorage.setItem(key(accountId), JSON.stringify({ ...readWorkspaceDestinations(accountId), [module]: path })) } catch { /* optional storage */ }
}
export function moduleDestination(to: string, destinations: WorkspaceDestinations) {
  const module = to === '/articles' ? 'articles' : to === '/articles?view=clip' ? 'clips' : to === '/explore' ? 'explore' : to === '/briefing' ? 'briefing' : null
  return module ? destinations[module] || to : to
}
