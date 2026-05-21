// Cross-page session-storage helpers for prev/next article navigation.
//
// When a list page opens an article, it snapshots the visible article IDs and
// (optionally) the fetch context needed to load the next page. ArticlePage uses
// the snapshot to render the prev/next buttons, and extends it on demand when
// the user clicks Next at the end of the list and more articles are loadable.
import { getArticles, getClip } from '../api/client'

type ArticlesParams = NonNullable<Parameters<typeof getArticles>[0]>
type ClipParams = NonNullable<Parameters<typeof getClip>[0]>

export type NavContext =
  | { kind: 'articles'; params: ArticlesParams; nextOffset: number; pageSize: number }
  | { kind: 'clip'; params: ClipParams; nextOffset: number; pageSize: number }

const NAV_LIST_KEY = 'articleNavList'
const NAV_CONTEXT_KEY = 'articleNavContext'

export function readNavList(): number[] {
  try {
    const raw = sessionStorage.getItem(NAV_LIST_KEY)
    return raw ? (JSON.parse(raw) as number[]) : []
  } catch {
    return []
  }
}

export function readNavContext(): NavContext | null {
  try {
    const raw = sessionStorage.getItem(NAV_CONTEXT_KEY)
    return raw ? (JSON.parse(raw) as NavContext) : null
  } catch {
    return null
  }
}

export function writeNav(navList: number[], context: NavContext | null) {
  try {
    sessionStorage.setItem(NAV_LIST_KEY, JSON.stringify(navList))
    if (context) sessionStorage.setItem(NAV_CONTEXT_KEY, JSON.stringify(context))
    else sessionStorage.removeItem(NAV_CONTEXT_KEY)
  } catch {
    // ignore storage failures (quota, disabled)
  }
}

export function clearNavContext() {
  try { sessionStorage.removeItem(NAV_CONTEXT_KEY) } catch {}
}

export async function fetchMoreIds(
  ctx: NavContext,
): Promise<{ ids: number[]; stillMore: boolean }> {
  if (ctx.kind === 'articles') {
    const data = await getArticles({ ...ctx.params, limit: ctx.pageSize, offset: ctx.nextOffset })
    const ids = (data || []).map(a => a.id)
    return { ids, stillMore: ids.length === ctx.pageSize }
  }
  const resp = await getClip({ ...ctx.params, limit: ctx.pageSize, offset: ctx.nextOffset })
  const ids = (resp.items || []).map(it => it.id)
  return { ids, stillMore: ids.length === ctx.pageSize }
}
