import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type {
  UsageResponse,
  CostResponse,
  Anomaly,
  UsagePeriod,
  UsageGroupBy,
} from '@/types/analytics'
import type { ApiResponse, ListResponse } from '@/types/sim'

const ANALYTICS_KEY = ['analytics'] as const
const ANOMALIES_KEY = ['anomalies'] as const

export interface UsageFilters {
  period: UsagePeriod
  from?: string
  to?: string
  group_by?: UsageGroupBy
  operator_id?: string
  apn_id?: string
  rat_type?: string
  compare?: boolean
}

export function useUsageAnalytics(filters: UsageFilters) {
  return useQuery({
    queryKey: [...ANALYTICS_KEY, 'usage', filters],
    queryFn: async () => {
      const params = new URLSearchParams()
      params.set('period', filters.period)
      if (filters.period === 'custom' && filters.from) params.set('from', filters.from)
      if (filters.period === 'custom' && filters.to) params.set('to', filters.to)
      if (filters.group_by) params.set('group_by', filters.group_by)
      if (filters.operator_id) params.set('operator_id', filters.operator_id)
      if (filters.apn_id) params.set('apn_id', filters.apn_id)
      if (filters.rat_type) params.set('rat_type', filters.rat_type)
      if (filters.compare) params.set('compare', 'true')
      const res = await api.get<ApiResponse<UsageResponse>>(
        `/analytics/usage?${params.toString()}`,
      )
      return res.data.data
    },
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
}

export interface CostFilters {
  period: UsagePeriod
  from?: string
  to?: string
  operator_id?: string
  apn_id?: string
  rat_type?: string
}

export function useCostAnalytics(filters: CostFilters) {
  return useQuery({
    queryKey: [...ANALYTICS_KEY, 'cost', filters],
    queryFn: async () => {
      const params = new URLSearchParams()
      params.set('period', filters.period)
      if (filters.period === 'custom' && filters.from) params.set('from', filters.from)
      if (filters.period === 'custom' && filters.to) params.set('to', filters.to)
      if (filters.operator_id) params.set('operator_id', filters.operator_id)
      if (filters.apn_id) params.set('apn_id', filters.apn_id)
      if (filters.rat_type) params.set('rat_type', filters.rat_type)
      const res = await api.get<ApiResponse<CostResponse>>(
        `/analytics/cost?${params.toString()}`,
      )
      return res.data.data
    },
    staleTime: 60_000,
  })
}

export interface AnomalyFilters {
  type?: string
  severity?: string
  state?: string
}

export function useAnomalyList(filters: AnomalyFilters) {
  return useInfiniteQuery({
    queryKey: [...ANOMALIES_KEY, 'list', filters],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '50')
      if (filters.type) params.set('type', filters.type)
      if (filters.severity) params.set('severity', filters.severity)
      if (filters.state) params.set('state', filters.state)
      const res = await api.get<ListResponse<Anomaly>>(
        `/analytics/anomalies?${params.toString()}`,
      )
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    staleTime: 15_000,
  })
}

export function useAnomalyStateUpdate() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ id, state }: { id: string; state: string }) => {
      const res = await api.patch<ApiResponse<Anomaly>>(
        `/analytics/anomalies/${id}`,
        { state },
      )
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ANOMALIES_KEY })
    },
  })
}
