import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type {
  RoamingAgreement,
  CreateRoamingAgreementRequest,
  UpdateRoamingAgreementRequest,
} from '@/types/roaming'

interface ListResponse<T> {
  status: string
  data: T[]
  meta: {
    cursor: string
    has_more: boolean
    limit: number
  }
}

interface ApiResponse<T> {
  status: string
  data: T
}

const ROAMING_KEY = ['roaming-agreements'] as const

export interface ListRoamingAgreementsParams {
  limit?: number
  cursor?: string
  operator_id?: string
  state?: string
  expiring_within_days?: number
}

export function useRoamingAgreements(params: ListRoamingAgreementsParams = {}) {
  const searchParams = new URLSearchParams()
  if (params.limit) searchParams.set('limit', String(params.limit))
  if (params.cursor) searchParams.set('cursor', params.cursor)
  if (params.operator_id) searchParams.set('operator_id', params.operator_id)
  if (params.state) searchParams.set('state', params.state)
  if (params.expiring_within_days) searchParams.set('expiring_within_days', String(params.expiring_within_days))

  const qs = searchParams.toString()
  return useQuery({
    queryKey: [...ROAMING_KEY, 'list', params],
    queryFn: async () => {
      const res = await api.get<ListResponse<RoamingAgreement>>(`/roaming-agreements${qs ? `?${qs}` : ''}`)
      return res.data
    },
    staleTime: 30_000,
  })
}

export function useRoamingAgreement(id: string) {
  return useQuery({
    queryKey: [...ROAMING_KEY, 'detail', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<RoamingAgreement>>(`/roaming-agreements/${id}`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 15_000,
  })
}

export function useOperatorRoamingAgreements(operatorId: string, params: { limit?: number; cursor?: string } = {}) {
  const searchParams = new URLSearchParams()
  if (params.limit) searchParams.set('limit', String(params.limit))
  if (params.cursor) searchParams.set('cursor', params.cursor)

  const qs = searchParams.toString()
  return useQuery({
    queryKey: [...ROAMING_KEY, 'by-operator', operatorId, params],
    queryFn: async () => {
      const res = await api.get<ListResponse<RoamingAgreement>>(
        `/operators/${operatorId}/roaming-agreements${qs ? `?${qs}` : ''}`,
      )
      return res.data
    },
    enabled: !!operatorId,
    staleTime: 30_000,
  })
}

export function useCreateRoamingAgreement() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: CreateRoamingAgreementRequest) => {
      const res = await api.post<ApiResponse<RoamingAgreement>>('/roaming-agreements', data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ROAMING_KEY })
    },
  })
}

export function useUpdateRoamingAgreement(id: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: UpdateRoamingAgreementRequest) => {
      const res = await api.patch<ApiResponse<RoamingAgreement>>(`/roaming-agreements/${id}`, data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ROAMING_KEY })
    },
  })
}

export function useTerminateRoamingAgreement(id: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      const res = await api.delete<ApiResponse<{ status: string }>>(`/roaming-agreements/${id}`)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ROAMING_KEY })
    },
  })
}
