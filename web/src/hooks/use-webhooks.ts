import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse, ListResponse } from '@/types/sim'

const WEBHOOKS_KEY = ['webhooks'] as const

export interface WebhookConfig {
  id: string
  tenant_id: string
  url: string
  event_types: string[]
  enabled: boolean
  last_success_at: string | null
  last_failure_at: string | null
  failure_count: number
  created_at: string
  updated_at: string
  secret?: string
}

export interface WebhookDelivery {
  id: string
  config_id: string
  event_type: string
  signature: string
  response_status: number | null
  response_body: string | null
  attempt_count: number
  next_retry_at: string | null
  final_state: string
  created_at: string
  updated_at: string
}

export function useWebhookConfigs(cursor: string = '', limit: number = 50) {
  return useQuery({
    queryKey: [...WEBHOOKS_KEY, 'list', cursor, limit],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (cursor) params.set('cursor', cursor)
      params.set('limit', String(limit))
      const res = await api.get<ListResponse<WebhookConfig>>(`/webhooks?${params.toString()}`)
      return res.data
    },
    staleTime: 30_000,
  })
}

export function useCreateWebhook() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (req: {
      url: string
      secret: string
      event_types: string[]
      enabled?: boolean
    }) => {
      const res = await api.post<ApiResponse<WebhookConfig>>('/webhooks', req)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: WEBHOOKS_KEY })
    },
  })
}

export function useUpdateWebhook() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({
      id,
      patch,
    }: {
      id: string
      patch: Partial<{ url: string; secret: string; event_types: string[]; enabled: boolean }>
    }) => {
      const res = await api.patch<ApiResponse<WebhookConfig>>(`/webhooks/${id}`, patch)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: WEBHOOKS_KEY })
    },
  })
}

export function useDeleteWebhook() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/webhooks/${id}`)
      return id
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: WEBHOOKS_KEY })
    },
  })
}

export function useWebhookDeliveries(configID: string | null, cursor: string = '', limit: number = 20) {
  return useQuery({
    queryKey: [...WEBHOOKS_KEY, 'deliveries', configID, cursor, limit],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (cursor) params.set('cursor', cursor)
      params.set('limit', String(limit))
      const res = await api.get<ListResponse<WebhookDelivery>>(
        `/webhooks/${configID}/deliveries?${params.toString()}`,
      )
      return res.data
    },
    enabled: !!configID,
    staleTime: 15_000,
  })
}

export function useRetryWebhookDelivery() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ configID, deliveryID }: { configID: string; deliveryID: string }) => {
      const res = await api.post<ApiResponse<WebhookDelivery>>(
        `/webhooks/${configID}/deliveries/${deliveryID}/retry`,
      )
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...WEBHOOKS_KEY, 'deliveries'] })
    },
  })
}
