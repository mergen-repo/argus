import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'
import type { Tenant } from '@/types/settings'

export interface TenantStats {
  sim_count: number
  apn_count: number
  user_count: number
  operator_count: number
  active_sessions: number
  monthly_cost: number
  storage_bytes: number
  quota_utilization?: number
  sims_quota?: number
  users_quota?: number
  api_keys_quota?: number
}

export function useTenantDetail(id: string | undefined) {
  return useQuery({
    queryKey: ['tenants', 'detail', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<Tenant>>(`/tenants/${id}`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 30_000,
  })
}

export function useTenantStats(id: string | undefined) {
  return useQuery({
    queryKey: ['tenants', 'stats', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<TenantStats>>(`/tenants/${id}/stats`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
}
