import { api } from './client'

export interface RouteParameter {
  description: string
  default?: string
  options?: { label: string; value: string }[]
}
export interface DirectoryRoute {
  id: string
  name: string
  path: string
  paths?: string[]
  categories: string[]
  description: string
  example: string
  parameters: Record<string, RouteParameter>
  requires_config: boolean
  config_names: string[]
  anti_crawler: boolean
  top_feeds: { title: string; url: string }[]
}
export interface DirectoryPlatform {
  id: string
  name: string
  url: string
  categories: string[]
  heat: number
  routes: DirectoryRoute[]
}
export interface FullSourceDirectory {
  source_url: string
  fetched_at: string
  stale: boolean
  platform_count: number
  route_count: number
  platforms: DirectoryPlatform[]
}
export const getSourceDirectory = (signal?: AbortSignal) => api.get<FullSourceDirectory>('/source-directory', { signal, timeout: 25000 }).then(response => response.data)
