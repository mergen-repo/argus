import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'
import type {
  DeviceBinding,
  IMEIHistoryFilters,
  IMEIHistoryListResponse,
} from '@/types/device-binding'

const DEVICE_BINDING_KEY = ['device-binding'] as const

export function useDeviceBinding(simId: string) {
  return useQuery({
    queryKey: [...DEVICE_BINDING_KEY, 'binding', simId],
    queryFn: async () => {
      const res = await api.get<ApiResponse<DeviceBinding>>(`/sims/${simId}/device-binding`)
      return res.data.data
    },
    enabled: !!simId,
    staleTime: 15_000,
  })
}

export function useRePair(simId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      const res = await api.post<ApiResponse<DeviceBinding>>(
        `/sims/${simId}/device-binding/re-pair`,
      )
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...DEVICE_BINDING_KEY, 'binding', simId] })
      queryClient.invalidateQueries({ queryKey: [...DEVICE_BINDING_KEY, 'history', simId] })
    },
  })
}

function buildHistoryParams(filters: IMEIHistoryFilters, cursor?: string) {
  const params = new URLSearchParams()
  if (cursor) params.set('cursor', cursor)
  params.set('limit', '50')
  if (filters.protocol) params.set('protocol', filters.protocol)
  if (filters.since) params.set('since', filters.since)
  return params.toString()
}

export function useIMEIHistory(simId: string, filters: IMEIHistoryFilters = {}) {
  return useInfiniteQuery({
    queryKey: [...DEVICE_BINDING_KEY, 'history', simId, filters],
    queryFn: async ({ pageParam }) => {
      const qs = buildHistoryParams(filters, pageParam as string)
      const res = await api.get<IMEIHistoryListResponse>(
        `/sims/${simId}/imei-history${qs ? `?${qs}` : ''}`,
      )
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.next_cursor : undefined,
    enabled: !!simId,
    staleTime: 10_000,
  })
}
