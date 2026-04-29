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
  is_read?: boolean
}

export interface ReadingProgress {
  id: number
  article_id: number
  scroll_position: number
  last_read_at: string
  is_completed: boolean
}

export interface InterestTopic {
  id: number
  topic: string
  weight: number
  last_reinforced_at: string
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

export const fetchFeedNow = (id: number) =>
  api.post<{ message: string; new_articles: number; feed_title: string }>(`/feeds/${id}/fetch`).then(res => res.data)

// Articles
export const getArticles = (params?: { feed_id?: number; unread?: boolean; limit?: number; offset?: number }) =>
  api.get<Article[]>('/articles', { params }).then(res => res.data)

export const getArticle = (id: number) =>
  api.get<{ article: Article; progress: ReadingProgress | null }>(`/articles/${id}`).then(res => res.data)

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

export const recordReadDuration = (articleId: number, durationSeconds: number) =>
  api.post('/preferences/read-duration', { article_id: articleId, duration_seconds: durationSeconds })

export const getTopics = () =>
  api.get<InterestTopic[]>('/preferences/topics').then(res => res.data)

export const generateInsights = () =>
  api.post<{ insights: string; message?: string }>('/insights/generate').then(res => res.data)

// Progress
export const getProgress = (articleId: number) =>
  api.get<{ progress: ReadingProgress | null }>(`/progress/${articleId}`).then(res => res.data)

export const updateProgress = (articleId: number, scrollPosition: number, isCompleted: boolean) =>
  api.post<ReadingProgress>(`/progress/${articleId}`, { scroll_position: scrollPosition, is_completed: isCompleted }).then(res => res.data)

export const resetProgress = (articleId: number) =>
  api.post(`/progress/${articleId}/reset`)

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

export const exportMarkdown = (articleId: number) =>
  api.get(`/articles/${articleId}/export/md`, { responseType: 'text' }).then(res => res.data as string)
