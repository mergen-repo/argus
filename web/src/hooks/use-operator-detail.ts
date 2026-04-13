import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'

export interface OperatorHealthEntry {
  checked_at: string
  status: string
  latency_ms: number | null
  circuit_state: string
  error_message: string | null
}

export interface OperatorMetricBucket {
  ts: string
  auth_rate_per_sec: number
  error_rate_per_sec: number
}

export interface OperatorMetricsResponse {
  window: string
  buckets: OperatorMetricBucket[]
}

export function useOperatorHealthHistory(id: string, hours: number = 24) {
  return useQuery({
    queryKey: ['operators', 'health-history', id, hours],
    queryFn: async () => {
      const res = await api.get<ApiResponse<OperatorHealthEntry[]>>(
        `/operators/${id}/health-history?hours=${hours}&limit=100`,
      )
      return res.data.data ?? []
    },
    enabled: !!id,
    staleTime: 15_000,
    refetchInterval: 30_000,
  })
}

export function useOperatorMetrics(id: string, window: string = '1h') {
  return useQuery({
    queryKey: ['operators', 'metrics', id, window],
    queryFn: async () => {
      const res = await api.get<ApiResponse<OperatorMetricsResponse>>(
        `/operators/${id}/metrics?window=${window}`,
      )
      return res.data.data
    },
    enabled: !!id,
    staleTime: 15_000,
    refetchInterval: 30_000,
  })
}
