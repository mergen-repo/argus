import { useQuery, useInfiniteQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { APN, IPPool, APNListFilters } from '@/types/apn'
import type { SIM, ListResponse, ApiResponse } from '@/types/sim'

const APNS_KEY = ['apns'] as const

function buildListParams(filters: APNListFilters, cursor?: string) {
  const params = new URLSearchParams()
  if (cursor) params.set('cursor', cursor)
  params.set('limit', '50')
  if (filters.operator_id) params.set('operator_id', filters.operator_id)
  if (filters.state) params.set('state', filters.state)
  return params.toString()
}

export function useAPNList(filters: APNListFilters) {
  return useQuery({
    queryKey: [...APNS_KEY, 'list', filters],
    queryFn: async () => {
      const qs = buildListParams(filters)
      const res = await api.get<ListResponse<APN>>(`/apns?${qs}`)
      return res.data.data
    },
    staleTime: 30_000,
  })
}

export function useAPN(id: string) {
  return useQuery({
    queryKey: [...APNS_KEY, 'detail', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<APN>>(`/apns/${id}`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 15_000,
  })
}

export function useAPNIPPools(apnId: string) {
  return useQuery({
    queryKey: [...APNS_KEY, 'ip-pools', apnId],
    queryFn: async () => {
      const res = await api.get<ListResponse<IPPool>>(`/ip-pools?apn_id=${apnId}&limit=100`)
      return res.data.data
    },
    enabled: !!apnId,
    staleTime: 30_000,
  })
}

export function useAPNSims(apnId: string) {
  return useInfiniteQuery({
    queryKey: [...APNS_KEY, 'sims', apnId],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '20')
      const res = await api.get<ListResponse<SIM>>(`/apns/${apnId}/sims?${params.toString()}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    enabled: !!apnId,
  })
}
