import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'

const PREFS_KEY = ['notification-preferences'] as const
const TEMPLATES_KEY = ['notification-templates'] as const

export interface NotificationPreference {
  event_type: string
  channels: string[]
  severity_threshold: string
  enabled: boolean
}

export interface NotificationTemplate {
  event_type: string
  locale: string
  subject: string
  body_text: string
  body_html: string
}

export function useNotificationPreferences() {
  return useQuery({
    queryKey: PREFS_KEY,
    queryFn: async () => {
      const res = await api.get<ApiResponse<{ preferences: NotificationPreference[] }>>(
        '/notification-preferences',
      )
      return res.data.data.preferences ?? []
    },
    staleTime: 30_000,
  })
}

export function useUpsertNotificationPreferences() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (prefs: NotificationPreference[]) => {
      await api.put('/notification-preferences', { preferences: prefs })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: PREFS_KEY })
    },
  })
}

export function useNotificationTemplates(eventType?: string, locale?: string) {
  return useQuery({
    queryKey: [...TEMPLATES_KEY, eventType ?? '', locale ?? ''],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (eventType) params.set('event_type', eventType)
      if (locale) params.set('locale', locale)
      const res = await api.get<ApiResponse<{ templates: NotificationTemplate[] }>>(
        `/notification-templates?${params.toString()}`,
      )
      return res.data.data.templates ?? []
    },
    staleTime: 60_000,
  })
}

export function useUpsertNotificationTemplate() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (t: NotificationTemplate) => {
      await api.put(`/notification-templates/${encodeURIComponent(t.event_type)}/${encodeURIComponent(t.locale)}`, {
        subject: t.subject,
        body_text: t.body_text,
        body_html: t.body_html,
      })
      return t
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: TEMPLATES_KEY })
    },
  })
}
