export function feedTypeLabel(type: string): string {
  return ({ rss: 'RSS', html: '网页', clip: '网摘', 'twitter:user': 'X 用户', 'twitter:list': 'X 列表', 'twitter:bookmarks': 'X 书签', link_set: '链接集' } as Record<string, string>)[type] || type
}
