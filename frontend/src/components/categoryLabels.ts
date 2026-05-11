// Canonical category enum and display labels, mirrored from
// backend model.ValidCategories. The first six match the values used by
// `recommended_feeds.category` (and originally lived in RecommendedPage.tsx);
// the last four were added in migration 019 for the /articles 分组 view.

export const CATEGORY_LABELS: Record<string, string> = {
  ai_eng: 'AI 工程',
  ai: 'AI',
  cn_tech: '中文科技',
  enterprise: '企业基建',
  youtube: '视频',
  podcast: '播客',
  news: '时事',
  blog: '博客随笔',
  health: '健康',
  business: '商业',
}

export const CATEGORY_ORDER = [
  'ai_eng', 'ai', 'cn_tech', 'enterprise', 'youtube', 'podcast',
  'news', 'blog', 'health', 'business',
]

// labelFor renders an enum slug into its Chinese display name, falling
// back to the raw slug if the map misses — covers prompt-output drift.
export function labelFor(slug: string): string {
  return CATEGORY_LABELS[slug] || slug
}
