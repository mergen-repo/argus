import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse, ListResponse } from '@/types/sim'
import type {
  DeliveryStatus,
  ActiveSession,
  SessionFilters,
  APIKeyUsageItem,
  PurgeHistoryItem,
  PurgeHistoryFilters,
} from '@/types/admin'

const DELIVERY_STATUS_KEY = (window: string) => ['admin', 'delivery-status', window] as const
const ACTIVE_SESSIONS_KEY = (filters: SessionFilters) => ['admin', 'active-sessions', filters] as const
const API_KEY_USAGE_KEY = (window: string) => ['admin', 'api-key-usage', window] as const
const PURGE_HISTORY_KEY = (filters: PurgeHistoryFilters) => ['admin', 'purge-history', filters] as const

export function useDeliveryStatus(window: '1h' | '24h' | '7d' | '30d' = '24h') {
  return useQuery({
    queryKey: DELIVERY_STATUS_KEY(window),
    queryFn: async () => {
      const res = await api.get<ApiResponse<DeliveryStatus>>(`/admin/delivery/status?window=${window}`)
      return res.data.data
    },
    staleTime: 60_000,
    refetchInterval: 60_000,
  })
}

export function useActiveSessions(filters: SessionFilters = {}) {
  return useQuery({
    queryKey: ACTIVE_SESSIONS_KEY(filters),
    queryFn: async () => {
      const params = new URLSearchParams()
      if (filters.tenant_id) params.set('tenant_id', filters.tenant_id)
      if (filters.cursor) params.set('cursor', filters.cursor)
      if (filters.limit) params.set('limit', String(filters.limit))
      const res = await api.get<ListResponse<ActiveSession>>(`/admin/sessions/active?${params.toString()}`)
      return res.data
    },
    staleTime: 15_000,
    refetchInterval: 30_000,
  })
}

export function useForceLogoutSession() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (sessionId: string) => {
      await api.post(`/admin/sessions/${sessionId}/revoke`, {})
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'active-sessions'] })
    },
  })
}

export function useAPIKeyUsage(window: '1h' | '24h' | '7d' | '30d' = '24h') {
  return useQuery({
    queryKey: API_KEY_USAGE_KEY(window),
    queryFn: async () => {
      const res = await api.get<ApiResponse<APIKeyUsageItem[]>>(`/admin/api-keys/usage?window=${window}`)
      return res.data.data ?? []
    },
    staleTime: 60_000,
  })
}

export function usePurgeHistory(filters: PurgeHistoryFilters = {}) {
  return useQuery({
    queryKey: PURGE_HISTORY_KEY(filters),
    queryFn: async () => {
      const params = new URLSearchParams()
      if (filters.tenant_id) params.set('tenant_id', filters.tenant_id)
      if (filters.from) params.set('from', filters.from)
      if (filters.to) params.set('to', filters.to)
      if (filters.cursor) params.set('cursor', filters.cursor)
      if (filters.limit) params.set('limit', String(filters.limit))
      const res = await api.get<ListResponse<PurgeHistoryItem>>(`/admin/purge-history?${params.toString()}`)
      return res.data
    },
    staleTime: 60_000,
  })
}
