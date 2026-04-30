import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { APN, IPPool, APNListFilters } from '@/types/apn'
import type { SIM, ListResponse, ApiResponse } from '@/types/sim'
import type { PolicyListItem } from '@/types/policy'

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

export interface CreateAPNData {
  name: string
  operator_id: string
  apn_type: string
  supported_rat_types: string[]
  display_name?: string
  ip_pool_ids?: string[]
}

export function useCreateAPN() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: CreateAPNData) => {
      const res = await api.post<ApiResponse<APN>>('/apns', data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: APNS_KEY })
    },
  })
}

export function useUpdateAPN(id: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: Partial<CreateAPNData>) => {
      const res = await api.patch<ApiResponse<APN>>(`/apns/${id}`, data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: APNS_KEY })
    },
  })
}

export function useDeleteAPN(id: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      await api.delete(`/apns/${id}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: APNS_KEY })
    },
  })
}

export function useAPNSims(apnId: string) {
  return useInfiniteQuery({
    queryKey: [...APNS_KEY, 'sims', apnId],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '20')
      params.set('apn_id', apnId)
      const res = await api.get<ListResponse<SIM>>(`/sims?${params.toString()}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    enabled: !!apnId,
  })
}

export function useAPNReferencingPolicies(apnId: string) {
  return useQuery({
    queryKey: [...APNS_KEY, 'referencing-policies', apnId],
    queryFn: async () => {
      const res = await api.get<ListResponse<PolicyListItem>>(`/apns/${apnId}/referencing-policies?limit=50`)
      return res.data.data
    },
    enabled: !!apnId,
    staleTime: 60_000,
  })
}

export function useCreateIPPool() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: {
      apn_id: string
      name: string
      cidr_v4?: string
      cidr_v6?: string
      alert_threshold_warning?: number
      alert_threshold_critical?: number
    }) => {
      const res = await api.post<ApiResponse<IPPool>>('/ip-pools', data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: APNS_KEY })
    },
  })
}
