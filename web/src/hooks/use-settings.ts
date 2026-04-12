import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useCallback } from 'react'
import { api } from '@/lib/api'
import { wsClient } from '@/lib/ws'
import type {
  TenantUser,
  ApiKey,
  ApiKeyCreateResult,
  IpPool,
  IpAddress,
  NotificationConfig,
  SystemMetrics,
  Tenant,
} from '@/types/settings'
import type { ListResponse, ApiResponse } from '@/types/sim'

const USERS_KEY = ['users'] as const
const API_KEYS_KEY = ['api-keys'] as const
const IP_POOLS_KEY = ['ip-pools'] as const
const NOTIF_CONFIG_KEY = ['notification-configs'] as const
const SYSTEM_KEY = ['system'] as const
const TENANTS_KEY = ['tenants'] as const
const RELIABILITY_KEY = ['reliability'] as const

export function useUserList() {
  return useQuery({
    queryKey: [...USERS_KEY, 'list'],
    queryFn: async () => {
      const res = await api.get<ListResponse<TenantUser & { state?: string }>>('/users?limit=200')
      return res.data.data.map((u) => ({
        ...u,
        status: (u.status ?? u.state ?? 'active') as TenantUser['status'],
      }))
    },
    staleTime: 30_000,
  })
}

export function useInviteUser() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (payload: { email: string; name: string; role: string }) => {
      const res = await api.post<ApiResponse<TenantUser>>('/users', payload)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: USERS_KEY })
    },
  })
}

export function useUpdateUser() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, ...payload }: { id: string; role?: string; status?: string }) => {
      const res = await api.patch<ApiResponse<TenantUser>>(`/users/${id}`, payload)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: USERS_KEY })
    },
  })
}

export function useApiKeyList() {
  return useQuery({
    queryKey: [...API_KEYS_KEY, 'list'],
    queryFn: async () => {
      const res = await api.get<ListResponse<ApiKey>>('/api-keys?limit=200')
      return res.data.data
    },
    staleTime: 30_000,
  })
}

export function useCreateApiKey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (payload: { name: string; scopes: string[]; rate_limit: number; expires_in_days?: number }) => {
      const res = await api.post<ApiResponse<ApiKeyCreateResult>>('/api-keys', payload)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: API_KEYS_KEY })
    },
  })
}

export function useRotateApiKey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      const res = await api.post<ApiResponse<ApiKeyCreateResult>>(`/api-keys/${id}/rotate`)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: API_KEYS_KEY })
    },
  })
}

export function useRevokeApiKey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/api-keys/${id}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: API_KEYS_KEY })
    },
  })
}

export function useIpPoolList() {
  return useQuery({
    queryKey: [...IP_POOLS_KEY, 'list'],
    queryFn: async () => {
      const res = await api.get<ListResponse<IpPool>>('/ip-pools?limit=200')
      return res.data.data
    },
    staleTime: 30_000,
  })
}

export function useIpPoolAddresses(poolId: string) {
  return useInfiniteQuery({
    queryKey: [...IP_POOLS_KEY, 'addresses', poolId],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '50')
      const res = await api.get<ListResponse<IpAddress>>(`/ip-pools/${poolId}/addresses?${params.toString()}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    enabled: !!poolId,
    staleTime: 15_000,
  })
}

export function useReserveIp() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ poolId, simId }: { poolId: string; simId: string }) => {
      const res = await api.post<ApiResponse<IpAddress>>(`/ip-pools/${poolId}/addresses/reserve`, { sim_id: simId })
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: IP_POOLS_KEY })
    },
  })
}

export function useReleaseIp() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ poolId, addressId }: { poolId: string; addressId: string }) => {
      await api.post(`/ip-pools/${poolId}/addresses/${addressId}/release`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: IP_POOLS_KEY })
    },
  })
}

export function useNotificationConfig() {
  return useQuery({
    queryKey: [...NOTIF_CONFIG_KEY],
    queryFn: async () => {
      const res = await api.get<ApiResponse<NotificationConfig>>('/notification-configs')
      return res.data.data
    },
    staleTime: 60_000,
  })
}

export function useUpdateNotificationConfig() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (config: NotificationConfig) => {
      const res = await api.put<ApiResponse<NotificationConfig>>('/notification-configs', config)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: NOTIF_CONFIG_KEY })
    },
  })
}

export function useSystemMetrics() {
  return useQuery({
    queryKey: [...SYSTEM_KEY, 'metrics'],
    queryFn: async () => {
      const res = await api.get<ApiResponse<SystemMetrics>>('/system/metrics')
      return res.data.data
    },
    staleTime: 5_000,
    refetchInterval: 10_000,
  })
}

export function useHealthCheck() {
  return useQuery({
    queryKey: [...SYSTEM_KEY, 'health'],
    queryFn: async () => {
      const res = await api.get<ApiResponse<{ services: { name: string; status: string; latency_ms: number }[] }>>('/health', { baseURL: '/api' })
      return res.data.data
    },
    staleTime: 10_000,
    refetchInterval: 15_000,
  })
}

export function useRealtimeMetrics() {
  const queryClient = useQueryClient()

  const handler = useCallback(
    (data: unknown) => {
      const d = data as { auth_per_sec?: number; active_sessions?: number; error_rate?: number }
      queryClient.setQueryData<SystemMetrics>([...SYSTEM_KEY, 'metrics'], (old) => {
        if (!old) return old
        return {
          ...old,
          ...(d.auth_per_sec !== undefined && { auth_per_sec: d.auth_per_sec }),
          ...(d.active_sessions !== undefined && { active_sessions: d.active_sessions }),
          ...(d.error_rate !== undefined && { error_rate: d.error_rate }),
        }
      })
    },
    [queryClient],
  )

  useEffect(() => {
    const unsub = wsClient.on('metrics.realtime', handler)
    return unsub
  }, [handler])
}

export function useHealthLive() {
  return useQuery({
    queryKey: [...SYSTEM_KEY, 'health-live'],
    queryFn: async () => {
      const res = await api.get<{ status: string; data: { status: string; uptime: string; goroutines: number; go_version: string } }>('/health/live', { baseURL: '/' })
      return res.data.data
    },
    staleTime: 10_000,
    refetchInterval: 15_000,
  })
}

export function useHealthReady() {
  return useQuery({
    queryKey: [...SYSTEM_KEY, 'health-ready'],
    queryFn: async () => {
      const res = await api.get<{
        status: string
        data: {
          state: string
          db: { status: string; latency_ms: number }
          redis: { status: string; latency_ms: number }
          nats: { status: string; latency_ms: number }
          aaa?: { radius: { status: string }; sessions_active: number }
          disks?: { mount: string; used_pct: number; status: string }[]
          uptime: string
          degraded_reasons?: string[]
        }
      }>('/health/ready', { baseURL: '/' })
      return res.data.data
    },
    staleTime: 10_000,
    refetchInterval: 15_000,
  })
}

export interface BackupRunEntry {
  status: string
  finished_at?: string
  size_mb: number
  s3_key: string
  sha256: string
  kind: string
  started_at: string
}

export interface BackupStatusData {
  last_daily?: BackupRunEntry
  last_weekly?: BackupRunEntry
  last_monthly?: BackupRunEntry
  last_verify?: {
    status: string
    verified_at: string
    tenants_count: number
    sims_count: number
  }
  history: BackupRunEntry[]
}

export function useBackupStatus() {
  return useQuery({
    queryKey: [...RELIABILITY_KEY, 'backup-status'],
    queryFn: async () => {
      const res = await api.get<{ status: string; data: BackupStatusData }>('/system/backup-status')
      return res.data.data
    },
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
}

export interface JWTRotationEntry {
  when: string
  actor: string
  correlation_id: string
}

export interface JWTRotationHistoryData {
  current_fingerprint: string
  previous_fingerprint: string
  history: JWTRotationEntry[]
}

export function useJwtRotationHistory() {
  return useQuery({
    queryKey: [...RELIABILITY_KEY, 'jwt-rotation'],
    queryFn: async () => {
      const res = await api.get<{ status: string; data: JWTRotationHistoryData }>('/system/jwt-rotation-history')
      return res.data.data
    },
    staleTime: 60_000,
  })
}

export function useTenantList() {
  return useQuery({
    queryKey: [...TENANTS_KEY, 'list'],
    queryFn: async () => {
      const res = await api.get<ListResponse<Tenant>>('/tenants?limit=200')
      return res.data.data
    },
    staleTime: 30_000,
  })
}

export function useCreateTenant() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (payload: { name: string; slug: string; plan: string; max_sims: number; max_users: number }) => {
      const res = await api.post<ApiResponse<Tenant>>('/tenants', payload)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: TENANTS_KEY })
    },
  })
}

export function useUpdateTenant() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, ...payload }: { id: string; name?: string; plan?: string; retention_days?: number; max_sims?: number; max_users?: number; max_api_keys?: number }) => {
      const res = await api.patch<ApiResponse<Tenant>>(`/tenants/${id}`, payload)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: TENANTS_KEY })
    },
  })
}
