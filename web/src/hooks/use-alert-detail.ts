import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { Anomaly } from '@/types/analytics'
import type { ApiResponse, ListResponse } from '@/types/sim'

const ANOMALIES_KEY = ['anomalies'] as const

export function useAlert(id: string | undefined) {
  return useQuery({
    queryKey: [...ANOMALIES_KEY, 'detail', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<Anomaly>>(`/analytics/anomalies/${id}`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 15_000,
    retry: (failureCount, error: unknown) => {
      const err = error as { status?: number }
      if (err?.status === 404) return false
      return failureCount < 2
    },
  })
}

export function useSimilarAlerts(type: string | undefined) {
  return useQuery({
    queryKey: [...ANOMALIES_KEY, 'similar', type],
    queryFn: async () => {
      const params = new URLSearchParams({ limit: '10' })
      if (type) params.set('type', type)
      const res = await api.get<ListResponse<Anomaly>>(`/analytics/anomalies?${params.toString()}`)
      return res.data.data ?? []
    },
    enabled: !!type,
    staleTime: 30_000,
  })
}

export function useUpdateAlertState() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, state, note }: { id: string; state: string; note?: string }) => {
      const res = await api.patch<ApiResponse<Anomaly>>(`/analytics/anomalies/${id}`, { state, note })
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ANOMALIES_KEY })
    },
  })
}
