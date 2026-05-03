import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse, ListResponse } from '@/types/sim'
import type {
  IMEIPool,
  IMEIPoolEntry,
  IMEIPoolListFilters,
  IMEIPoolAddPayload,
  IMEIPoolBulkImportResponse,
} from '@/types/imei-pool'

const IMEI_POOLS_KEY = ['imei-pools'] as const

function buildListParams(filters: IMEIPoolListFilters, cursor?: string) {
  const params = new URLSearchParams()
  if (cursor) params.set('cursor', cursor)
  params.set('limit', '50')
  if (filters.kind) params.set('kind', filters.kind)
  if (filters.tac) params.set('tac', filters.tac)
  if (filters.device_model) params.set('device_model', filters.device_model)
  if (filters.q) params.set('q', filters.q)
  if (filters.include_bound_count) params.set('include_bound_count', '1')
  return params.toString()
}

export function useIMEIPoolList(pool: IMEIPool, filters: Omit<IMEIPoolListFilters, 'pool'> = {}) {
  return useInfiniteQuery({
    queryKey: [...IMEI_POOLS_KEY, 'list', pool, filters],
    queryFn: async ({ pageParam }) => {
      const qs = buildListParams({ ...filters, pool, include_bound_count: true }, pageParam as string)
      const res = await api.get<ListResponse<IMEIPoolEntry>>(`/imei-pools/${pool}?${qs}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    staleTime: 15_000,
  })
}

export function useIMEIPoolAdd(pool: IMEIPool) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (payload: IMEIPoolAddPayload) => {
      const body: Record<string, unknown> = {
        kind: payload.kind,
        imei_or_tac: payload.imei_or_tac,
      }
      if (payload.device_model !== undefined && payload.device_model !== null) {
        body.device_model = payload.device_model
      }
      if (payload.description !== undefined && payload.description !== null) {
        body.description = payload.description
      }
      if (payload.quarantine_reason !== undefined && payload.quarantine_reason !== null) {
        body.quarantine_reason = payload.quarantine_reason
      }
      if (payload.block_reason !== undefined && payload.block_reason !== null) {
        body.block_reason = payload.block_reason
      }
      if (payload.imported_from !== undefined && payload.imported_from !== null) {
        body.imported_from = payload.imported_from
      }
      const res = await api.post<ApiResponse<IMEIPoolEntry>>(`/imei-pools/${pool}`, body)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...IMEI_POOLS_KEY, 'list', pool] })
    },
  })
}

export function useIMEIPoolDelete(pool: IMEIPool) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/imei-pools/${pool}/${id}`)
      return id
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...IMEI_POOLS_KEY, 'list', pool] })
    },
  })
}

export function useIMEIPoolBulkImport(pool: IMEIPool) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (file: File) => {
      const formData = new FormData()
      formData.append('file', file)
      const res = await api.post<ApiResponse<IMEIPoolBulkImportResponse>>(
        `/imei-pools/${pool}/import`,
        formData,
        { headers: { 'Content-Type': 'multipart/form-data' } },
      )
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...IMEI_POOLS_KEY, 'list', pool] })
    },
  })
}
