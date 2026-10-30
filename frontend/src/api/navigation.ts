import { apiClient } from './client'

export type NavigationSection = 'news' | 'reset' | 'models' | 'lottery' | 'admin_lottery' | 'pages'
export interface NavigationStatus {
  tickets: number
  checkin_needed: boolean
  pending_wins: number
  unopened_boxes: number
  activities: string[]
  news?: string[]
  reset?: string[]
  seen: Partial<Record<NavigationSection, string[]>>
}
export interface NavigationBadge {
  count: number
  dot: boolean
  label: string
}

export const navigationAPI = {
  async status(admin = false, signal?: AbortSignal) {
    return (await apiClient.get<NavigationStatus>(`${admin ? '/admin' : ''}/workspace/navigation`, { signal })).data
  },
  async read(section: NavigationSection, keys: string[], admin = false) {
    return (await apiClient.post<{ keys: string[] }>(`${admin ? '/admin' : ''}/workspace/navigation/read`, { section, keys })).data
  }
}
