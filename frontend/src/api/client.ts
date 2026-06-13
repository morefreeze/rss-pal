import axios, { AxiosError, InternalAxiosRequestConfig } from 'axios'
import { clearAllFabCollapsed } from '../components/CollapsibleFab'

// Per-request retry counter so the interceptor never retries more than once.
// __refreshed marks the request as already replayed after a refresh-token
// exchange so we never loop refresh→401→refresh.
interface RetriableConfig extends InternalAxiosRequestConfig {
  __retryCount?: number
  __refreshed?: boolean
}

export const api = axios.create({
  baseURL: '/api',
  // Weak networks can stall single packets for many seconds. 10s is
  // generous for legitimate slow responses while still letting the
  // interceptor retry on outright network failure.
  timeout: 10000,
})

// Single-flight refresh: if multiple in-flight requests all 401 at once, we
// only POST /auth/refresh ONCE and share the resulting promise. Each request
// then replays with the new access JWT.
let refreshInFlight: Promise<string | null> | null = null

async function tryRefreshAccessToken(): Promise<string | null> {
  const refresh = localStorage.getItem('refresh_token')
  if (!refresh) return null
  if (refreshInFlight) return refreshInFlight
  refreshInFlight = (async () => {
    try {
      // Bypass the JWT interceptor (no token, plain axios call) — refresh
      // never carries the (expired) Authorization header.
      const res = await axios.post<{ token: string }>('/api/auth/refresh', { refresh_token: refresh })
      if (res.data?.token) {
        localStorage.setItem('token', res.data.token)
        return res.data.token
      }
      return null
    } catch {
      return null
    } finally {
      refreshInFlight = null
    }
  })()
  return refreshInFlight
}

// Clears all auth state. Called when both access JWT and refresh token have
// been rejected (or no refresh token was issued in the first place).
function clearAuthLocal() {
  localStorage.removeItem('token')
  localStorage.removeItem('user')
  localStorage.removeItem('refresh_token')
  localStorage.removeItem('refresh_token_expires_at')
}

// JWT interceptor
api.interceptors.request.use(config => {
  // Don't clobber a per-request Authorization (e.g. bookmarklet-token
  // calls like capturePDFURL set their own Bearer that the JWT layer
  // should not override).
  if (config.headers?.Authorization) {
    return config
  }
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  res => {
    const fresh = res.headers?.['x-new-token']
    if (typeof fresh === 'string' && fresh) {
      localStorage.setItem('token', fresh)
    }
    return res
  },
  async (err: AxiosError) => {
    const config = err.config as RetriableConfig | undefined

    // Retry once on network failure / timeout — GET only. Non-GET
    // retries could create duplicate side-effects.
    if (config) {
      const method = (config.method ?? 'get').toLowerCase()
      const isNetworkFailure = !err.response || err.code === 'ECONNABORTED' || err.code === 'ERR_NETWORK'
      if (method === 'get' && isNetworkFailure && !config.__retryCount) {
        config.__retryCount = 1
        await new Promise(r => setTimeout(r, 500))
        return api(config)
      }
    }

    if (err.response?.status === 401) {
      // Bookmarklet-token endpoints authenticate with a per-request
      // Bearer token, NOT the JWT in localStorage. A 401 from one of
      // these calls means the bookmarklet token is bad/expired — it
      // does NOT mean the user's session is invalid, so don't blow
      // away localStorage or redirect to /login.
      const url = err.config?.url || ''
      if (url.startsWith('/bookmarklet/')) {
        return Promise.reject(err)
      }
      // Don't try to refresh on refresh itself, or on requests we've already
      // replayed once after a refresh.
      if (config && !config.__refreshed && !url.startsWith('/auth/refresh')) {
        const fresh = await tryRefreshAccessToken()
        if (fresh) {
          config.__refreshed = true
          // Inject the new token (request interceptor would also pick it up,
          // but being explicit avoids races with header overrides).
          config.headers = config.headers ?? {}
          ;(config.headers as Record<string, string>).Authorization = `Bearer ${fresh}`
          return api(config)
        }
      }
      clearAuthLocal()
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

export const login = (username: string, password: string, remember = false) =>
  api.post('/auth/login', { username, password, remember }).then(res => {
    localStorage.setItem('token', res.data.token)
    localStorage.setItem('user', JSON.stringify(res.data.user))
    if (res.data.refresh_token) {
      localStorage.setItem('refresh_token', res.data.refresh_token)
      if (res.data.refresh_token_expires_at) {
        localStorage.setItem('refresh_token_expires_at', res.data.refresh_token_expires_at)
      }
    } else {
      // User unchecked "remember" on re-login — drop any stale refresh token.
      localStorage.removeItem('refresh_token')
      localStorage.removeItem('refresh_token_expires_at')
    }
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

export const getServerHealth = () =>
  api.get<{ status: string; version: string }>('/health').then(res => res.data)

export const changePassword = (oldPassword: string, newPassword: string) =>
  api.put('/auth/password', { old_password: oldPassword, new_password: newPassword }).then(res => res.data)

export const logout = () => {
  // Best-effort server-side revoke so the refresh token can't be reused
  // if it's been stolen. Fire-and-forget; never block the UX on the call.
  const refresh = localStorage.getItem('refresh_token')
  if (refresh) {
    // Use a bare axios call so the request interceptor doesn't override
    // the now-stale Authorization header; the access JWT isn't required
    // for logout anyway.
    void axios.post('/api/auth/logout', { refresh_token: refresh }).catch(() => {})
  }
  clearAuthLocal()
  // Clear session-local state so a new login gets a clean slate
  sessionStorage.removeItem('readArticles')
  sessionStorage.removeItem('articleNavList')
  sessionStorage.removeItem('articleNavContext')
  sessionStorage.removeItem('articleListScroll')
  sessionStorage.removeItem('selectedFeed')
  sessionStorage.removeItem('unreadOnly')
  sessionStorage.removeItem('savedOnly')
  clearAllFabCollapsed()
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
  expand_links: boolean
}

// ArticleListItem is the lean shape returned by GET /api/articles — the
// list endpoint deliberately drops `content` and `summary_detailed` to
// keep the wire payload small on weak networks. The detail view fetches
// the full Article via GET /api/articles/:id when the user opens one.
export interface ArticleListItem {
  id: number
  feed_id: number
  feed_title?: string
  title: string
  url: string
  published_at: string | null
  summary_brief: string
  fetched_at: string
  word_count?: number
  reading_minutes?: number
  is_read?: boolean
  media_url?: string
  media_type?: string
  media_duration_seconds?: number
  // link_set fields
  links_extendable?: boolean | null  // tri-state: null = unchecked, true/false = checked
  link_set_suggested?: boolean | null  // worker thinks article is a link list, awaiting user confirmation
  parent_article_id?: number | null
  processing_state?: 'ready' | 'stub' | 'processing' | 'failed'
  processing_error?: string
  prerank_score?: number | null
  editor_note?: string
  manual_tags: UserTag[]
  // Transient/derived markers that may decorate list items in other
  // endpoints reusing this shape (e.g. recommended/link_set fallback).
  parent_title?: string
  is_fallback?: boolean
  is_link_set?: boolean
  kind?: 'article' | 'tweet' | 'tweet_thread'
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
  // link_set fields
  is_link_set?: boolean
  links_extendable?: boolean | null  // tri-state: null = unchecked, true/false = checked
  link_set_suggested?: boolean | null  // worker thinks article is a link list, awaiting user confirmation
  parent_article_id?: number | null
  parent_title?: string  // populated only by GET /articles/recommended/link_set
  is_fallback?: boolean  // true = surfaced by quality-fallback (may be already-read)
  processing_state?: 'ready' | 'stub' | 'processing' | 'failed'
  processing_error?: string
  prerank_score?: number | null
  editor_note?: string
  manual_tags: UserTag[]
  kind?: 'article' | 'tweet' | 'tweet_thread'
  // Per-image intrinsic dimensions, keyed by the original (pre-proxy) URL
  // as it appears in markdown. Used to render <img width=W height=H ...>
  // so the browser reserves layout space before lazy-loaded images decode.
  image_dimensions?: Record<string, [number, number]>
}

export interface CandidateView {
  title: string
  url: string
  editor_note?: string
  already_fetched: boolean
}

export async function getArticleCandidates(articleId: number): Promise<CandidateView[]> {
  const { data } = await api.get<{ candidates: CandidateView[] }>(`/articles/${articleId}/candidates`)
  return data.candidates ?? []
}

export async function batchFetchCandidates(
  articleId: number,
  candidates: Array<{ title: string; url: string; editor_note?: string }>
): Promise<{ inserted: number }> {
  const { data } = await api.post(`/articles/${articleId}/batch_fetch`, { candidates })
  return data
}

export async function confirmLinkSetSuggestion(articleId: number): Promise<void> {
  await api.post(`/articles/${articleId}/confirm_link_set`)
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
  discovered_rss_url?: string
}

export interface ArticleDetailResponse {
  article: Article
  progress?: any
  signals?: any
  from_bookmarklet?: boolean
  hidden?: boolean
  children?: Article[]
}

// Feeds
export const getFeeds = () =>
  api.get<Feed[]>('/feeds').then(res => res.data)

export const previewFeed = (url: string) =>
  api.post<FeedPreview>('/feeds/preview', { url }).then(res => res.data)

export const addFeed = (url: string, feedType?: string, expandLinks: boolean = false) =>
  api.post<Feed>('/feeds', { url, feed_type: feedType || 'rss', expand_links: expandLinks }).then(res => res.data)

// PDF capture — server-side fetch of a PDF URL routed through the
// bookmarklet capture pipeline. Auth is the bookmarklet token (NOT a
// JWT), passed explicitly so the existing JWT interceptor doesn't
// override it. Response status maps directly to the worker state:
//   created    — brand new article, fast extract succeeded
//   updated    — existing article re-extracted
//   processing — sync extract found no text, OCR queued
export interface PDFCaptureResponse {
  status: 'created' | 'updated' | 'processing'
  article_id: number
  message: string
}

export async function capturePDFURL(url: string, bookmarkletToken: string): Promise<PDFCaptureResponse> {
  const { data } = await api.post<PDFCaptureResponse>(
    '/bookmarklet/capture-pdf-url',
    { url },
    { headers: { Authorization: `Bearer ${bookmarkletToken}` } },
  )
  return data
}

// getMyBookmarkletToken returns the current user's bookmarklet token or
// throws if the user hasn't generated one. Thin wrapper over the
// existing getBookmarkletToken helper that turns the "no token yet"
// null into a thrown error so the caller can surface a friendly hint.
export async function getMyBookmarkletToken(): Promise<string> {
  const token = await getBookmarkletToken()
  if (!token) {
    throw new Error('请先在「设置 → 一键收藏书签」处生成 bookmarklet token')
  }
  return token
}

export const deleteFeed = (id: number) =>
  api.delete(`/feeds/${id}`)

export const toggleFeedActive = (id: number, isActive: boolean, title: string) =>
  api.put<Feed>(`/feeds/${id}`, { is_active: isActive, title }).then(res => res.data)

export const fetchFeedNow = (id: number) =>
  api.post<{ message: string; new_articles: number; feed_title: string }>(`/feeds/${id}/fetch`).then(res => res.data)

export const exportOPML = () =>
  api.get('/feeds/export/opml', { responseType: 'blob' }).then(res => res.data as Blob)

// Articles
export type ArticleSort = 'published' | 'captured'
export type ArticleOrder = 'asc' | 'desc'

export const getArticles = (params?: {
  feed_id?: number
  unread?: boolean
  saved?: boolean
  tag_id?: number
  untagged?: boolean
  limit?: number
  offset?: number
  sort?: ArticleSort
  order?: ArticleOrder
}) => api.get<ArticleListItem[]>('/articles', { params }).then(res => res.data)

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
  api.get<ArticleListItem[]>('/articles/search', { params: { q, limit } }).then(res => res.data)

export const getUnreadCount = () =>
  api.get<{ count: number }>('/articles/unread-count').then(res => res.data.count)

export const markAllRead = (filters?: {
  feedId?: number | null
  unread?: boolean
  saved?: boolean
  // Clip-mode filters mirror /api/clip so 网摘 view can mark-all-read
  // exactly what the user currently sees under the active tag/source.
  tagIds?: number[]
  mode?: 'and' | 'or'
  untagged?: boolean
  source?: string
}) =>
  api.post('/articles/mark-all-read', null, {
    params: {
      feed_id: filters?.feedId ?? undefined,
      unread: filters?.unread ? 'true' : undefined,
      saved: filters?.saved ? 'true' : undefined,
      untagged: filters?.untagged ? 'true' : undefined,
      tag_ids: filters?.tagIds && filters.tagIds.length > 0 ? filters.tagIds.join(',') : undefined,
      mode: filters?.tagIds && filters.tagIds.length > 1 ? filters.mode : undefined,
      source: filters?.source || undefined,
    },
  }).then(res => res.data)

export const getArticle = (id: number) =>
  api.get<ArticleDetailResponse>(`/articles/${id}`).then(res => res.data)

export const getRecommended = (limit?: number) =>
  api.get<Article[]>('/articles/recommended', { params: { limit } }).then(res => res.data)

// link_set API methods
export interface OneoffLinkSetResponse {
  feed_id: number
  parent_article_id: number
}

export const createOneoffLinkSet = (url: string, expand: boolean): Promise<OneoffLinkSetResponse> =>
  api.post<OneoffLinkSetResponse>('/feeds/oneoff_link_set', { url, expand }).then(res => res.data)

export const expandLinkSetChild = (articleId: number): Promise<{ article_id: number; state: string }> =>
  api.post(`/articles/${articleId}/expand`).then(res => res.data)

export const getLinkSetRecommendations = (days: number = 7, limit: number = 20): Promise<Article[]> =>
  api.get<Article[]>('/articles/recommended/link_set', { params: { days, limit } }).then(res => res.data)

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

// Per-user soft delete ("hide"). The article row stays in the DB; the user
// just stops seeing it in their own lists. Reversed by unhideArticle.
export const hideArticle = (articleId: number) =>
  api.post<{ hidden: true; hidden_at: string }>(`/articles/${articleId}/hide`).then(res => res.data)

export const unhideArticle = (articleId: number) =>
  api.delete<{ hidden: false }>(`/articles/${articleId}/hide`).then(res => res.data)

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
  opts?: { forceVision?: boolean },
): Promise<void> {
  const token = localStorage.getItem('token')
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'Accept': 'application/x-ndjson',
  }
  if (token) headers['Authorization'] = `Bearer ${token}`

  const body = templateId ? JSON.stringify({ template_id: templateId }) : '{}'

  const qp = new URLSearchParams({ stream: '1' })
  if (opts?.forceVision) qp.set('force_vision', '1')

  let resp: Response
  try {
    resp = await fetch(`/api/articles/${articleId}/summary?${qp.toString()}`, {
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
  has_saved: boolean
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

// RestoreUploadInput is one of:
//   - { archive: File }                       — single .tar.gz / .tgz bundle
//   - { metadata: File; saved?: File | null } — raw .json (+ optional sibling)
export type RestoreUploadInput =
  | { archive: File }
  | { metadata: File; saved?: File | null }

// restoreBackupUpload sends a user-picked local backup to the server. The
// server writes the bytes to a temp dir, restores, then deletes — backup.Dir
// on the host is not touched.
export const restoreBackupUpload = (input: RestoreUploadInput) => {
  const form = new FormData()
  if ('archive' in input) {
    form.append('archive', input.archive)
  } else {
    form.append('metadata', input.metadata)
    if (input.saved) form.append('saved', input.saved)
  }
  return api
    .post<{ ok: boolean; stats: BackupRestoreStats }>('/admin/backups/restore-upload', form)
    .then(res => res.data)
}

// downloadBackup fetches the backup as a Blob via the auth'd axios client
// (plain <a download> can't carry Bearer headers) and triggers a browser
// save. When the backup has a saved-archive sibling, the server bundles both
// files into one .tar.gz; otherwise the plain .json is returned. The output
// filename follows server-set Content-Disposition when present.
export const downloadBackup = async (metadataName: string, hasSaved: boolean): Promise<void> => {
  const res = await api.get<Blob>(`/admin/backups/download/${encodeURIComponent(metadataName)}`, {
    responseType: 'blob',
  })
  const dispo = res.headers['content-disposition'] || ''
  const m = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(dispo)
  const filename = m?.[1] ? decodeURIComponent(m[1]) : (hasSaved ? metadataName.replace(/\.json$/, '.tar.gz') : metadataName)
  const url = URL.createObjectURL(res.data)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export interface WeeklyDigest {
  week_start: string
  intro_text: string
  articles: Article[]
  pending?: boolean
}

export const getWeeklyDigest = (week?: string) =>
  api.get<WeeklyDigest>('/weekly-digest', { params: week ? { week } : {} }).then(res => res.data)

export interface DailyDigest {
  requested_date: string
  shown_date: string
  pending: boolean
  intro_text: string
  articles: Article[]
  mode: 'cached' | 'live' | 'pending'
}

export const getDailyDigest = (date?: string) =>
  api.get<DailyDigest>('/daily-digest', { params: date ? { date } : {} }).then(r => r.data)

export type BriefingTab = 'daily' | 'weekly'

export const getBriefingLastTab = () =>
  api.get<{ tab: BriefingTab }>('/briefing/last-tab').then(r => r.data)

export const setBriefingLastTab = (tab: BriefingTab) =>
  api.post('/briefing/last-tab', { tab })

export interface BriefingIndex {
  type: 'daily' | 'weekly'
  today_label?: string         // present when type='daily'
  this_week_start?: string     // present when type='weekly'
  pending_window_start: string
  cached: string[]
}

export const getBriefingIndex = (type: 'daily' | 'weekly', from: string, to: string) =>
  api.get<BriefingIndex>('/briefing/index', { params: { type, from, to } }).then(r => r.data)

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

export interface TagSidebarData {
  tags: UserTag[]
  total_count: number
  untagged_count: number
}

export const getTagSidebar = (params?: {
  feed_id?: number
  unread?: boolean
  saved?: boolean
}) => api.get<TagSidebarData>('/tags/sidebar', { params }).then(r => r.data)

// === Clip (网摘) ===

// EffectiveSource: what the source-tag chip on a clip article should say.
// `key` is "feed:<id>" or "host:<host>" — opaque to the UI, passed back as
// the `source` filter on /api/clip.
export interface EffectiveSource {
  key: string
  title: string
}

export type ClipItem = Article & {
  manual_tags: UserTag[]
  effective_source: EffectiveSource
}

export interface ClipListResponse {
  items: ClipItem[]
  total: number
}

export interface GetClipParams {
  tag_ids?: number[]
  mode?: 'and' | 'or'
  untagged?: boolean
  source?: string // EffectiveSource.key, e.g. "feed:8" or "host:github.com"
  limit?: number
  offset?: number
  sort?: ArticleSort
  order?: ArticleOrder
  unread?: boolean
  saved?: boolean
}

export const getClip = (params: GetClipParams = {}) => {
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
  if (params.sort) query.sort = params.sort
  if (params.order) query.order = params.order
  if (params.unread) query.unread = 'true'
  if (params.saved) query.saved = 'true'
  return api.get<ClipListResponse>('/clip', { params: query }).then(r => r.data)
}
