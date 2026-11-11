export type WorkspaceModule = 'articles' | 'clips' | 'explore' | 'briefing'
export interface WorkspaceRoute {
  module: WorkspaceModule
  cacheKey: string
  listPath: string
  returnPath: string
  articleId: number | null
}

/** Accept only local list routes; never feed an arbitrary history origin to Routes. */
export function safeListPath(value: unknown): string | null {
  if (typeof value !== 'string' || /[\\#\r\n]/.test(value)) return null
  if (!/^\/(articles|clip|explore|daily|weekly)(\?[^#]*)?$/.test(value)) return null
  return value
}

export function resolveWorkspaceRoute(pathname: string, search: string, state?: unknown): WorkspaceRoute | null {
  const match = pathname.match(/^\/(articles|explore\/articles)\/(\d+)$/)
  let listPath = safeListPath(pathname + search)
  if (match) {
    const origin = safeListPath((state as { from?: unknown } | null)?.from)
    if (match[1] === 'explore/articles') {
      listPath = origin?.split('?')[0] === '/explore' ? origin : '/explore'
    } else {
      listPath = origin || '/articles'
      if (listPath.split('?')[0] === '/explore') listPath = '/articles'
    }
  }
  if (!listPath) return null
  const [path, query] = listPath.split('?')
  const module: WorkspaceModule = path === '/explore' ? 'explore'
    : path === '/daily' || path === '/weekly' ? 'briefing'
    : path === '/clip' || new URLSearchParams(query).get('view') === 'clip' ? 'clips' : 'articles'
  const returnPath = match && (state as { from?: unknown } | null)?.from === '/history' ? '/history' : listPath
  return { returnPath, module, cacheKey: path === '/daily' || path === '/weekly' || path === '/clip' ? path.slice(1) : module, listPath, articleId: match ? Number(match[2]) : null }
}
