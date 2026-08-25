export type ArticleTitleLocationState = {
  articlePreview?: {
    id?: number
    title?: string
  }
}

const APP_TITLE = 'RSS Pal'

const STATIC_ROUTE_TITLES: Record<string, string> = {
  '/login': '登录',
  '/register': '注册',
  '/share': '分享文章',
  '/extension-config': '浏览器扩展配置',
  '/feeds': '订阅源',
  '/feeds/health': '订阅健康',
  '/daily': '每日简报',
  '/weekly': '每周简报',
  '/clip': '网摘',
  '/interests': '兴趣',
  '/insights': '兴趣',
  '/stats': '统计',
  '/settings': '设置',
}

function cleanTitle(title: string | undefined | null): string {
  return (title ?? '').replace(/\s+/g, ' ').trim()
}

export function buildDocumentTitle(pageTitle: string): string {
  const title = cleanTitle(pageTitle)
  if (!title || title === APP_TITLE) return APP_TITLE
  return `${title} - ${APP_TITLE}`
}

export function getRoutePageTitle(
  pathname: string,
  search = '',
  state?: ArticleTitleLocationState | null,
): string {
  if (pathname === '/articles') {
    const params = new URLSearchParams(search)
    if (params.get('view') === 'clip') return '网摘'
    if (params.get('saved') === '1') return '已保存文章'
    if (params.get('feed_id')) return '订阅文章'
    return '文章列表'
  }

  const articleMatch = pathname.match(/^\/articles\/(\d+)$/)
  if (articleMatch) {
    const articleId = Number(articleMatch[1])
    const preview = state?.articlePreview
    if (preview?.id === articleId) {
      const previewTitle = cleanTitle(preview.title)
      if (previewTitle) return previewTitle
    }
    return `文章 ${articleId}`
  }

  if (pathname.startsWith('/share/')) return STATIC_ROUTE_TITLES['/share']

  return STATIC_ROUTE_TITLES[pathname] ?? APP_TITLE
}
