import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse, ListResponse } from '@/types/sim'
import type {
  KillSwitch,
  ToggleKillSwitchRequest,
  MaintenanceWindow,
  CreateMaintenanceWindowRequest,
  DeliveryStatus,
  TenantResourceItem,
  TenantQuota,
  CostTenant,
  ActiveSession,
  SessionFilters,
  APIKeyUsageItem,
  DSARQueueItem,
  DSARFilters,
  PurgeHistoryItem,
  PurgeHistoryFilters,
} from '@/types/admin'

const KILL_SWITCHES_KEY = ['admin', 'kill-switches'] as const
const MAINTENANCE_WINDOWS_KEY = (active?: boolean) => ['admin', 'maintenance-windows', { active }] as const
const DELIVERY_STATUS_KEY = (window: string) => ['admin', 'delivery-status', window] as const
const TENANT_RESOURCES_KEY = ['admin', 'tenant-resources'] as const
const TENANT_QUOTAS_KEY = ['admin', 'tenant-quotas'] as const
const COST_BY_TENANT_KEY = ['admin', 'cost-by-tenant'] as const
const ACTIVE_SESSIONS_KEY = (filters: SessionFilters) => ['admin', 'active-sessions', filters] as const
const API_KEY_USAGE_KEY = (window: string) => ['admin', 'api-key-usage', window] as const
const DSAR_QUEUE_KEY = (filters: DSARFilters) => ['admin', 'dsar-queue', filters] as const
const PURGE_HISTORY_KEY = (filters: PurgeHistoryFilters) => ['admin', 'purge-history', filters] as const

export function useKillSwitches() {
  return useQuery({
    queryKey: KILL_SWITCHES_KEY,
    queryFn: async () => {
      const res = await api.get<ApiResponse<KillSwitch[]>>('/admin/kill-switches')
      return res.data.data ?? []
    },
    staleTime: 15_000,
  })
}

export function useToggleKillSwitch() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ key, payload }: { key: string; payload: ToggleKillSwitchRequest }) => {
      const res = await api.patch<ApiResponse<KillSwitch>>(`/admin/kill-switches/${key}`, payload)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: KILL_SWITCHES_KEY })
    },
  })
}

export function useMaintenanceWindows(active?: boolean) {
  return useQuery({
    queryKey: MAINTENANCE_WINDOWS_KEY(active),
    queryFn: async () => {
      const params = new URLSearchParams()
      if (active !== undefined) params.set('active', String(active))
      const res = await api.get<ApiResponse<MaintenanceWindow[]>>(`/admin/maintenance-windows?${params.toString()}`)
      return res.data.data ?? []
    },
    staleTime: 30_000,
  })
}

export function useCreateMaintenanceWindow() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (payload: CreateMaintenanceWindowRequest) => {
      const res = await api.post<ApiResponse<MaintenanceWindow>>('/admin/maintenance-windows', payload)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'maintenance-windows'] })
    },
  })
}

export function useDeleteMaintenanceWindow() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/admin/maintenance-windows/${id}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'maintenance-windows'] })
    },
  })
}

export function useDeliveryStatus(window: '1h' | '24h' | '7d' = '24h') {
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

export function useTenantResources() {
  return useQuery({
    queryKey: TENANT_RESOURCES_KEY,
    queryFn: async () => {
      const res = await api.get<ApiResponse<TenantResourceItem[]>>('/admin/tenants/resources')
      return res.data.data ?? []
    },
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
}

export function useTenantQuotas() {
  return useQuery({
    queryKey: TENANT_QUOTAS_KEY,
    queryFn: async () => {
      const res = await api.get<ApiResponse<TenantQuota[]>>('/admin/tenants/quotas')
      return res.data.data ?? []
    },
    staleTime: 30_000,
  })
}

export function useCostByTenant() {
  return useQuery({
    queryKey: COST_BY_TENANT_KEY,
    queryFn: async () => {
      const res = await api.get<ApiResponse<CostTenant[]>>('/admin/cost/by-tenant')
      return res.data.data ?? []
    },
    staleTime: 300_000,
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

export function useAPIKeyUsage(window: '1h' | '24h' | '7d' = '24h') {
  return useQuery({
    queryKey: API_KEY_USAGE_KEY(window),
    queryFn: async () => {
      const res = await api.get<ApiResponse<APIKeyUsageItem[]>>(`/admin/api-keys/usage?window=${window}`)
      return res.data.data ?? []
    },
    staleTime: 60_000,
  })
}

export function useDSARQueue(filters: DSARFilters = {}) {
  return useQuery({
    queryKey: DSAR_QUEUE_KEY(filters),
    queryFn: async () => {
      const params = new URLSearchParams()
      if (filters.status) params.set('status', filters.status)
      if (filters.cursor) params.set('cursor', filters.cursor)
      if (filters.limit) params.set('limit', String(filters.limit))
      const res = await api.get<ListResponse<DSARQueueItem>>(`/admin/dsar/queue?${params.toString()}`)
      return res.data
    },
    staleTime: 30_000,
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
