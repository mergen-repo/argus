import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse, ListResponse } from '@/types/sim'

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

export interface OperatorSessionRow {
  id: string
  sim_id: string
  operator_id: string
  apn_id?: string
  nas_ip: string
  framed_ip?: string
  imsi?: string
  iccid?: string
  msisdn?: string
  session_state: string
  started_at: string
  duration_sec: number
  bytes_in: number
  bytes_out: number
}

export function useOperatorSessions(id: string, limit: number = 50) {
  return useQuery({
    queryKey: ['operators', 'sessions', id, limit],
    queryFn: async () => {
      const res = await api.get<ListResponse<OperatorSessionRow>>(
        `/operators/${id}/sessions?limit=${limit}`,
      )
      return res.data.data ?? []
    },
    enabled: !!id,
    staleTime: 10_000,
    refetchInterval: 15_000,
  })
}

export interface OperatorTrafficBucket {
  ts: string
  bytes_in: number
  bytes_out: number
  auth_count: number
}

export function useOperatorTraffic(id: string, period: string = '24h') {
  return useQuery({
    queryKey: ['operators', 'traffic', id, period],
    queryFn: async () => {
      const res = await api.get<ApiResponse<{ period: string; series: OperatorTrafficBucket[] }>>(
        `/operators/${id}/traffic?period=${period}`,
      )
      return res.data.data.series ?? []
    },
    enabled: !!id,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
}
