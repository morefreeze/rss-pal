import axios from 'axios'

export const api = axios.create({
  baseURL: '/api',
})

// JWT interceptor
api.interceptors.request.use(config => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  res => res,
  err => {
    if (err.response?.status === 401) {
      localStorage.removeItem('token')
      localStorage.removeItem('user')
      window.location.href = '/login'
    }
    return Promise.reject(err)
  }
)

// Auth
export const initAdmin = (password: string) =>
  api.post('/auth/init', { password }).then(res => {
    localStorage.setItem('token', res.data.token)
    localStorage.setItem('user', JSON.stringify(res.data.user))
    return res.data
  })

export const login = (username: string, password: string) =>
  api.post('/auth/login', { username, password }).then(res => {
    localStorage.setItem('token', res.data.token)
    localStorage.setItem('user', JSON.stringify(res.data.user))
    return res.data
  })

export const register = (username: string, password: string, code: string) =>
  api.post('/auth/register', { username, password, code }).then(res => {
    localStorage.setItem('token', res.data.token)
    localStorage.setItem('user', JSON.stringify(res.data.user))
    return res.data
  })

export const getMe = () =>
  api.get<{ id: number; username: string; is_admin: boolean }>('/auth/me').then(res => res.data)

export const changePassword = (oldPassword: string, newPassword: string) =>
  api.put('/auth/password', { old_password: oldPassword, new_password: newPassword }).then(res => res.data)

export const logout = () => {
  localStorage.removeItem('token')
  localStorage.removeItem('user')
  // Clear session-local state so a new login gets a clean slate
  sessionStorage.removeItem('readArticles')
  sessionStorage.removeItem('articleNavList')
  sessionStorage.removeItem('articleListScroll')
  sessionStorage.removeItem('selectedFeed')
  sessionStorage.removeItem('unreadOnly')
  sessionStorage.removeItem('savedOnly')
}

export const getUser = () => {
  const u = localStorage.getItem('user')
  return u ? JSON.parse(u) : null
}

export const isLoggedIn = () => !!localStorage.getItem('token')

// Invite codes
export const createInviteCode = (expiresInHours = 72) =>
  api.post('/auth/invite-codes', { expires_in_hours: expiresInHours }).then(res => res.data)

export const getInviteCodes = () =>
  api.get<InviteCode[]>('/auth/invite-codes').then(res => res.data)

// Types
export interface Feed {
  id: number
  url: string
  title: string
  last_fetched_at: string | null
  fetch_interval_minutes: number
  is_active: boolean
  owner_id: number | null
  feed_type: string
  created_at: string
  article_count: number
  unread_count: number
}

export interface Article {
  id: number
  feed_id: number
  feed_title?: string
  title: string
  url: string
  content: string
  published_at: string | null
  summary_brief: string
  summary_detailed: string
  fetched_at: string
  word_count?: number
  reading_minutes?: number
  is_read?: boolean
  media_url?: string
  media_type?: string
  media_duration_seconds?: number
}

export interface ReadingProgress {
  id: number
  article_id: number
  scroll_position: number
  last_read_at: string
  is_completed: boolean
}

export interface PlaybackProgress {
  position_seconds: number
  is_completed: boolean
}

export interface InterestTopic {
  id: number
  topic: string
  weight: number
  last_reinforced_at: string
}

export interface InterestTag {
  id: number
  tag: string
  weight: number
  last_reinforced_at: string
}

export interface ArticleRecommendation {
  article_id: number
  reason: string
}

export interface RecommendationDirection {
  direction: string
  direction_kind: 'core' | 'emerging'
  articles: ArticleRecommendation[]
}

export interface RecArticleMeta {
  id: number
  title: string
  feed_title: string
  brief: string
  is_read: boolean
}

export interface PersistedInsight {
  id: number
  content: string
  status: 'pending' | 'done' | 'failed'
  error_msg?: string
  triggered_by: 'auto' | 'manual'
  model?: string
  generated_at: string
  recommendations?: RecommendationDirection[]
}

export interface InsightsLatest {
  insight: PersistedInsight | null
  remaining_today: number
  remaining_month: number
  rec_articles?: Record<string, RecArticleMeta>
}

export interface InviteCode {
  id: number
  code: string
  created_by: number
  used_by: number | null
  expires_at: string | null
  created_at: string
}

// Feed preview types
export interface FeedPreviewItem {
  title: string
  url: string
  published_at?: string
}

export interface FeedPreview {
  feed_title: string
  feed_type: 'rss' | 'html'
  actual_url: string
  items: FeedPreviewItem[]
}

// Feeds
export const getFeeds = () =>
  api.get<Feed[]>('/feeds').then(res => res.data)

export const previewFeed = (url: string) =>
  api.post<FeedPreview>('/feeds/preview', { url }).then(res => res.data)

export const addFeed = (url: string, feedType?: string) =>
  api.post<Feed>('/feeds', { url, feed_type: feedType || 'rss' }).then(res => res.data)

export const deleteFeed = (id: number) =>
  api.delete(`/feeds/${id}`)

export const toggleFeedActive = (id: number, isActive: boolean, title: string) =>
  api.put<Feed>(`/feeds/${id}`, { is_active: isActive, title }).then(res => res.data)

export const fetchFeedNow = (id: number) =>
  api.post<{ message: string; new_articles: number; feed_title: string }>(`/feeds/${id}/fetch`).then(res => res.data)

export const exportOPML = () =>
  api.get('/feeds/export/opml', { responseType: 'blob' }).then(res => res.data as Blob)

// Articles
export const getArticles = (params?: { feed_id?: number; unread?: boolean; saved?: boolean; limit?: number; offset?: number }) =>
  api.get<Article[]>('/articles', { params }).then(res => res.data)

export interface TopicGroup {
  topic: string
  total_count: number
  articles: Article[]
}

export interface GroupedArticles {
  groups: TopicGroup[]
  unclassified: TopicGroup
}

export const getGroupedArticles = (params?: { feed_id?: number; unread?: boolean; saved?: boolean }) =>
  api.get<GroupedArticles>('/articles/grouped', { params }).then(res => res.data)

export const searchArticles = (q: string, limit?: number) =>
  api.get<Article[]>('/articles/search', { params: { q, limit } }).then(res => res.data)

export const getUnreadCount = () =>
  api.get<{ count: number }>('/articles/unread-count').then(res => res.data.count)

export const markAllRead = (filters?: { feedId?: number | null; unread?: boolean; saved?: boolean }) =>
  api.post('/articles/mark-all-read', null, {
    params: {
      feed_id: filters?.feedId ?? undefined,
      unread: filters?.unread ? 'true' : undefined,
      saved: filters?.saved ? 'true' : undefined,
    },
  }).then(res => res.data)

export const getArticle = (id: number) =>
  api.get<{ article: Article; progress: ReadingProgress | null; signals: Record<string, number> | null; from_bookmarklet?: boolean }>(`/articles/${id}`).then(res => res.data)

export const getRecommended = (limit?: number) =>
  api.get<Article[]>('/articles/recommended', { params: { limit } }).then(res => res.data)

export const generateSummary = (id: number) =>
  api.post<{ summary_brief: string; summary_detailed: string }>(`/articles/${id}/summary`).then(res => res.data)

export const fetchContent = (id: number) =>
  api.post<{ content: string }>(`/articles/${id}/content`).then(res => res.data)

// Preferences
export const likeArticle = (articleId: number) =>
  api.post('/preferences/like', { article_id: articleId })

export const dislikeArticle = (articleId: number) =>
  api.post('/preferences/dislike', { article_id: articleId })

export const saveArticle = (articleId: number) =>
  api.post('/preferences/save', { article_id: articleId })

export const unsaveArticle = (articleId: number) =>
  api.delete('/preferences/save', { data: { article_id: articleId } })

export const recordReadDuration = (articleId: number, durationSeconds: number) =>
  api.post('/preferences/read-duration', { article_id: articleId, duration_seconds: durationSeconds })

export const getTopics = () =>
  api.get<InterestTopic[]>('/preferences/topics').then(res => res.data)

export const getLatestInsights = () =>
  api.get<InsightsLatest>('/insights/latest').then(res => res.data)

export interface GenerateInsightsResp {
  status: 'pending' | 'no_data'
  id?: number
  message?: string
  remaining_today: number
  remaining_month: number
}

// generateInsights kicks off an async insight job. Returns immediately;
// poll /insights/latest to observe transition from pending → done|failed.
// Throws on HTTP error (e.g. 429 quota_exceeded, 409 already_pending).
export const generateInsights = () =>
  api.post<GenerateInsightsResp>('/insights/generate').then(res => res.data)

export const getTags = () =>
  api.get<InterestTag[]>('/preferences/tags').then(res => res.data)

export const deleteTopic = (id: number) =>
  api.delete(`/preferences/topics/${id}`)

export const deleteInterestTag = (id: number) =>
  api.delete(`/preferences/tags/${id}`)

// Progress
export const getProgress = (articleId: number) =>
  api.get<{ progress: ReadingProgress | null }>(`/progress/${articleId}`).then(res => res.data)

export const updateProgress = (articleId: number, scrollPosition: number, isCompleted: boolean) =>
  api.post<ReadingProgress>(`/progress/${articleId}`, { scroll_position: scrollPosition, is_completed: isCompleted }).then(res => res.data)

export const resetProgress = (articleId: number) =>
  api.post(`/progress/${articleId}/reset`)

export const getPlayback = (articleId: number) =>
  api.get<PlaybackProgress>(`/articles/${articleId}/playback`).then(res => res.data)

export const putPlayback = (articleId: number, body: PlaybackProgress) =>
  api.put(`/articles/${articleId}/playback`, body).then(() => undefined)

// Stats
export const getStats = () =>
  api.get<FeedStats>('/stats').then(res => res.data)

export const getFetchProgress = () =>
  api.get<FetchProgress[]>('/stats/progress').then(res => res.data)

export interface FeedStats {
  total_feeds: number
  active_feeds: number
  total_articles: number
  today_articles: number
  with_content: number
  without_content: number
  with_summary: number
}

export interface FetchProgress {
  feed_id: number
  feed_title: string
  feed_url: string
  last_fetched_at: string | null
  article_count: number
  content_progress: number
  summary_progress: number
}

// Templates
export interface SummaryTemplate {
  id: number
  user_id: number | null
  name: string
  description: string
  style: string
  brief_prompt: string
  detailed_prompt: string
  is_system: boolean
  created_at: string
}

export interface UserAIConfig {
  id?: number
  api_key: string
  base_url: string
  model: string
}

export interface ShareInfo {
  token: string
  url: string
}

export const getTemplates = () =>
  api.get<SummaryTemplate[]>('/templates').then(res => res.data)

export const createTemplate = (t: Partial<SummaryTemplate>) =>
  api.post<SummaryTemplate>('/templates', t).then(res => res.data)

export const deleteTemplate = (id: number) =>
  api.delete(`/templates/${id}`)

export const getAIConfig = () =>
  api.get<UserAIConfig>('/settings/ai').then(res => res.data)

export const saveAIConfig = (cfg: UserAIConfig) =>
  api.put<UserAIConfig>('/settings/ai', cfg).then(res => res.data)

export const setDefaultTemplate = (templateId: number) =>
  api.put('/settings/template', { template_id: templateId })

export const shareArticle = (articleId: number) =>
  api.post<ShareInfo>(`/articles/${articleId}/share`).then(res => res.data)

export const generateSummaryWithTemplate = (articleId: number, templateId?: number) =>
  api.post(`/articles/${articleId}/summary`, templateId ? { template_id: templateId } : {}).then(res => res.data)

export type SummaryStreamHandlers = {
  onBriefDelta?: (text: string) => void
  onBriefDone?: (full: string) => void
  onBriefPhaseDone?: () => void
  onDetailedDelta?: (text: string) => void
  onDetailedDone?: (full: string) => void
  onError?: (msg: string) => void
  onDone?: () => void
}

export async function generateSummaryStream(
  articleId: number,
  templateId: number | undefined,
  handlers: SummaryStreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const token = localStorage.getItem('token')
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'Accept': 'application/x-ndjson',
  }
  if (token) headers['Authorization'] = `Bearer ${token}`

  const body = templateId ? JSON.stringify({ template_id: templateId }) : '{}'

  let resp: Response
  try {
    resp = await fetch(`/api/articles/${articleId}/summary?stream=1`, {
      method: 'POST',
      credentials: 'include',
      headers,
      body,
      signal,
    })
  } catch (e: any) {
    if (e?.name !== 'AbortError') handlers.onError?.(e?.message || 'network error')
    return
  }

  if (!resp.ok || !resp.body) {
    const text = await resp.text().catch(() => '')
    handlers.onError?.(text || `HTTP ${resp.status}`)
    return
  }

  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      let nl = buf.indexOf('\n')
      while (nl !== -1) {
        const line = buf.slice(0, nl).trim()
        buf = buf.slice(nl + 1)
        if (line) dispatchSummaryFrame(line, handlers)
        nl = buf.indexOf('\n')
      }
    }
    if (buf.trim()) dispatchSummaryFrame(buf.trim(), handlers)
  } catch (e: any) {
    if (e?.name === 'AbortError') return
    handlers.onError?.(e?.message || 'stream error')
  }
}

function dispatchSummaryFrame(line: string, h: SummaryStreamHandlers) {
  let frame: any
  try { frame = JSON.parse(line) } catch { return }
  switch (frame.type) {
    case 'brief_delta': h.onBriefDelta?.(frame.text || ''); break
    case 'brief_phase_done': h.onBriefPhaseDone?.(); break
    case 'brief_done': h.onBriefDone?.(frame.text || ''); break
    case 'detailed_delta': h.onDetailedDelta?.(frame.text || ''); break
    case 'detailed_done': h.onDetailedDone?.(frame.text || ''); break
    case 'error': h.onError?.(frame.msg || 'unknown error'); break
    case 'done': h.onDone?.(); break
  }
}

export const exportMarkdown = (articleId: number) =>
  api.get(`/articles/${articleId}/export/md`, { responseType: 'text' }).then(res => res.data as string)

export const polishPrompt = (content: string) =>
  api.post<{ polished: string }>('/settings/polish-prompt', { content }).then(res => res.data.polished)

export const getBookmarkletToken = () =>
  api.get<{ token: string | null }>('/settings/bookmarklet-token').then(res => res.data.token)

export const regenerateBookmarkletToken = () =>
  api.post<{ token: string }>('/settings/bookmarklet-token/regenerate').then(res => res.data.token)

// Admin: subscription backup + restore (admin-only).
export interface BackupFile {
  name: string
  created_at: string
  size: number
}
export interface BackupListResponse {
  backups: BackupFile[]
  dir: string
}
export interface BackupRestoreStats {
  feeds: number
  user_tags: number
  interest_categories: number
  interest_topics: number
  skipped_article_link: number
}
export const listBackups = () =>
  api.get<BackupListResponse>('/admin/backups').then(res => res.data)

export const createBackupNow = () =>
  api.post<{ ok: boolean }>('/admin/backups').then(res => res.data)

export const restoreBackup = (name: string) =>
  api.post<{ ok: boolean; stats: BackupRestoreStats }>('/admin/backups/restore', { name }).then(res => res.data)

export interface RecommendedFeed {
  id: number
  url: string
  title: string
  description: string
  category: string                // 'ai_eng' | 'cn_tech' | 'enterprise' | 'podcast' | 'youtube'
  language: string                // 'zh' | 'en'
  feed_type: string
  is_broken: boolean
  sort_order: number
  subscribed: boolean
  created_at: string
}

export const getRecommendedFeeds = () =>
  api.get<RecommendedFeed[]>('/recommended-feeds').then(res => res.data || [])

export const subscribeRecommendedFeed = (id: number) =>
  api.post<{ status: string; feed_id?: number }>(`/recommended-feeds/${id}/subscribe`).then(res => res.data)

export interface WeeklyDigest {
  week_start: string
  intro_text: string
  articles: Article[]
}

export const getWeeklyDigest = (week?: string) =>
  api.get<WeeklyDigest>('/weekly-digest', { params: week ? { week } : {} }).then(res => res.data)

// === Feed governance Phase 1 ===

export type EventType = 'exposure' | 'click'

export const postEvent = (articleId: number, eventType: EventType) =>
  api.post('/events', { article_id: articleId, event_type: eventType })

export interface PruningRule {
  id: 'R1' | 'R2' | 'R3' | 'R4' | 'R5'
  label: string
  reason: string
  suggested_actions: string[]
}

export interface FeedHealthRow {
  feed_id: number
  feed_title: string
  status: 'active' | 'paused' | 'archived'
  priority_weight: number
  produced: number
  exposures: number
  clicks: number
  completed_reads: number
  ctr: number | null
  read_completion: number | null
  avg_duration_min: number
  feedback_density: number
  last_active_at: string | null
  last_fetched_at: string | null
  value_score: number | null
  pruning_rule?: PruningRule | null
}

export interface FeedHealthKPI {
  total_active: number
  healthy: number
  dormant: number
  completed_reads_w: number
}

export interface ArchivedFeed {
  feed_id: number
  feed_title: string
}

export interface FeedHealthResponse {
  window: '30d' | '90d'
  kpi: FeedHealthKPI
  rows: FeedHealthRow[]
  archived: ArchivedFeed[]
}

export const getFeedHealth = (window: '30d' | '90d' = '30d') =>
  api.get<FeedHealthResponse>(`/feeds/health?window=${window}`).then(r => r.data)

export const updateFeedStatus = (feedId: number, status: 'active' | 'paused' | 'archived') =>
  api.patch(`/feeds/${feedId}/status`, { status })

export const updateFeedWeight = (feedId: number, weight: number) =>
  api.patch(`/feeds/${feedId}/weight`, { priority_weight: weight })

// === Tags ===

export interface UserTag {
  id: number
  user_id: number
  name: string
  created_at: string
  article_count: number
}

export interface ArticleTagSource {
  feed_id: number
  title: string
}

export interface ArticleTagsResponse {
  source: ArticleTagSource
  manual: UserTag[]
  suggestions: string[]
}

export const listTags = () => api.get<UserTag[]>('/tags').then(r => r.data)
export const createTag = (name: string) =>
  api.post<{ id: number; name: string }>('/tags', { name }).then(r => r.data)
export const renameTag = (id: number, name: string) =>
  api.patch(`/tags/${id}`, { name })
export const deleteTag = (id: number) => api.delete(`/tags/${id}`)

export const getArticleTags = (articleId: number) =>
  api.get<ArticleTagsResponse>(`/articles/${articleId}/tags`).then(r => r.data)
export const addArticleTag = (articleId: number, name: string) =>
  api.post<{ id: number; name: string }>(`/articles/${articleId}/tags`, { name }).then(r => r.data)
export const removeArticleTag = (articleId: number, tagId: number) =>
  api.delete(`/articles/${articleId}/tags/${tagId}`)

export const dismissSuggestion = (articleId: number, name: string) =>
  api.post(`/articles/${articleId}/suggestions/dismiss`, { name })

// === Saved (Phase 2) ===

// EffectiveSource: what the source-tag chip on a saved article should say.
// `key` is "feed:<id>" or "host:<host>" — opaque to the UI, passed back as
// the `source` filter on /api/saved.
export interface EffectiveSource {
  key: string
  title: string
}

export type SavedItem = Article & {
  manual_tags: UserTag[]
  effective_source: EffectiveSource
}

export interface SavedListResponse {
  items: SavedItem[]
  total: number
}

export interface GetSavedParams {
  tag_ids?: number[]
  mode?: 'and' | 'or'
  untagged?: boolean
  source?: string // EffectiveSource.key, e.g. "feed:8" or "host:github.com"
  limit?: number
  offset?: number
}

export const getSaved = (params: GetSavedParams = {}) => {
  const query: Record<string, string | number | boolean> = {}
  if (params.untagged) {
    query.untagged = 'true'
  } else if (params.tag_ids && params.tag_ids.length > 0) {
    query.tag_ids = params.tag_ids.join(',')
    if (params.tag_ids.length > 1 && params.mode) {
      query.mode = params.mode
    }
  }
  if (params.source) query.source = params.source
  if (params.limit !== undefined) query.limit = params.limit
  if (params.offset !== undefined) query.offset = params.offset
  return api.get<SavedListResponse>('/saved', { params: query }).then(r => r.data)
}
