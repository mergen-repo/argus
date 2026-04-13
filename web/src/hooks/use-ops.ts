import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type {
  OpsMetricsSnapshot,
  InfraHealth,
  Incident,
  AnomalyComment,
  EscalateRequest,
  IncidentFilters,
} from '@/types/ops'
import type { ApiResponse, ListResponse } from '@/types/sim'

const OPS_SNAPSHOT_KEY = ['ops', 'snapshot'] as const
const OPS_INFRA_KEY = ['ops', 'infra-health'] as const
const OPS_INCIDENTS_KEY = ['ops', 'incidents'] as const
const ANOMALY_COMMENTS_KEY = (anomalyId: string) => ['anomaly-comments', anomalyId] as const

export function useOpsSnapshot(refreshMs = 15_000) {
  return useQuery({
    queryKey: OPS_SNAPSHOT_KEY,
    queryFn: async () => {
      const res = await api.get<ApiResponse<OpsMetricsSnapshot>>('/ops/metrics/snapshot')
      return res.data.data
    },
    refetchInterval: refreshMs,
    staleTime: refreshMs / 2,
  })
}

export function useInfraHealth(refreshMs = 10_000) {
  return useQuery({
    queryKey: OPS_INFRA_KEY,
    queryFn: async () => {
      const res = await api.get<ApiResponse<InfraHealth>>('/ops/infra-health')
      return res.data.data
    },
    refetchInterval: refreshMs,
    staleTime: refreshMs / 2,
  })
}

export function useIncidents(filters: IncidentFilters = {}) {
  return useQuery({
    queryKey: [...OPS_INCIDENTS_KEY, filters],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (filters.from) params.set('from', filters.from)
      if (filters.to) params.set('to', filters.to)
      if (filters.severity) params.set('severity', filters.severity)
      if (filters.state) params.set('state', filters.state)
      if (filters.entity_id) params.set('entity_id', filters.entity_id)
      if (filters.cursor) params.set('cursor', filters.cursor)
      if (filters.limit) params.set('limit', String(filters.limit))
      const res = await api.get<ListResponse<Incident>>(`/ops/incidents?${params.toString()}`)
      return res.data
    },
    staleTime: 30_000,
  })
}

export function useAnomalyComments(anomalyId: string) {
  return useQuery({
    queryKey: ANOMALY_COMMENTS_KEY(anomalyId),
    queryFn: async () => {
      const res = await api.get<ApiResponse<AnomalyComment[]>>(`/analytics/anomalies/${anomalyId}/comments`)
      return res.data.data ?? []
    },
    enabled: !!anomalyId,
    staleTime: 30_000,
  })
}

export function useAddAnomalyComment(anomalyId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: string) => {
      const res = await api.post<ApiResponse<AnomalyComment>>(`/analytics/anomalies/${anomalyId}/comments`, { body })
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ANOMALY_COMMENTS_KEY(anomalyId) })
    },
  })
}

export function useEscalateAnomaly(anomalyId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (payload: EscalateRequest) => {
      const res = await api.post(`/analytics/anomalies/${anomalyId}/escalate`, payload)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['anomalies'] })
    },
  })
}

export function useDeployHistory(filters: { from?: string; to?: string; deployer?: string; cursor?: string; limit?: number } = {}) {
  return useQuery({
    queryKey: ['deploy-history', filters],
    queryFn: async () => {
      const params = new URLSearchParams({ entity_type: 'deployment' })
      if (filters.from) params.set('from', filters.from)
      if (filters.to) params.set('to', filters.to)
      if (filters.cursor) params.set('cursor', filters.cursor)
      if (filters.limit) params.set('limit', String(filters.limit))
      const res = await api.get(`/audit?${params.toString()}`)
      return res.data
    },
    staleTime: 60_000,
  })
}
