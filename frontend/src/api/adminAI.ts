import { api } from './client'

export interface AISelection { provider: string; model: string }
export interface AICredential { endpoint: string; api_key: string }
export interface AIProvider {
  id: string
  name: string
  endpoints: { id: string; name: string }[]
  endpoint: string
  has_key: boolean
}
export interface AdminAIConfig {
  revision: number
  threshold: number
  small: AISelection
  large: AISelection
  providers: AIProvider[]
}
export interface AIModel { id: string; name: string }
export const getAdminAI = (signal?: AbortSignal) =>
  api.get<AdminAIConfig>('/admin/ai', { signal }).then(res => res.data)
export const saveAdminAI = (input: Pick<AdminAIConfig, 'revision' | 'threshold' | 'small' | 'large'> & { credentials: Record<string, AICredential> }) =>
  api.put<AdminAIConfig>('/admin/ai', input).then(res => res.data)
export const getAdminAIModels = (input: { provider: string; endpoint: string; api_key?: string }, signal?: AbortSignal) =>
  api.post<{ models: AIModel[]; source: string }>('/admin/ai/models', input, { signal, timeout: 30000 }).then(res => res.data)
