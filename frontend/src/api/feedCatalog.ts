import { api } from './client'

export interface CatalogFields {
  title: string
  url: string
  category: string
  description: string
  sort_order: number
}
export interface CatalogItem extends CatalogFields {
  id: number
  published: boolean
  check_status: 'unchecked' | 'ok' | 'failed'
  last_checked_at: string | null
  last_error: string
  revision: number
  created_at: string
  updated_at: string
}
export type PublicCatalogItem = CatalogFields & { id: number }
export const listFeedCatalog = () => api.get<PublicCatalogItem[]>('/feed-catalog').then(r => r.data)
export const listAdminFeedCatalog = () => api.get<CatalogItem[]>('/admin/feed-catalog').then(r => r.data)
export const createCatalogItem = (fields: CatalogFields) => api.post<CatalogItem>('/admin/feed-catalog', fields).then(r => r.data)
export const updateCatalogItem = (id: number, fields: CatalogFields & { revision: number }) => api.put<CatalogItem>(`/admin/feed-catalog/${id}`, fields).then(r => r.data)
// Feed validation can outlast the normal 10-second API timeout.
export const checkCatalogItem = (id: number, revision: number) => api.post<CatalogItem>(`/admin/feed-catalog/${id}/check`, { revision }, { timeout: 90000 }).then(r => r.data)
export const publishCatalogItem = (id: number, published: boolean, revision: number) => api.post<CatalogItem>(`/admin/feed-catalog/${id}/publication`, { published, revision }, { timeout: 90000 }).then(r => r.data)
