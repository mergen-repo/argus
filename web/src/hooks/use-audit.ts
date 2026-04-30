import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { AuditLog, AuditVerifyResult } from '@/types/audit'
import type { ListResponse, ApiResponse } from '@/types/sim'

const AUDIT_KEY = ['audit-logs'] as const

export interface AuditFilters {
  action?: string
  entity_type?: string
  entity_id?: string
  user_id?: string
  from?: string
  to?: string
}

export function useAuditList(filters: AuditFilters) {
  return useInfiniteQuery({
    queryKey: [...AUDIT_KEY, 'list', filters],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '50')
      if (filters.action) params.set('action', filters.action)
      if (filters.entity_type) params.set('entity_type', filters.entity_type)
      if (filters.entity_id) params.set('entity_id', filters.entity_id)
      if (filters.user_id) params.set('user_id', filters.user_id)
      if (filters.from) params.set('from', filters.from)
      if (filters.to) params.set('to', filters.to)
      const res = await api.get<ListResponse<AuditLog>>(`/audit?${params.toString()}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    staleTime: 15_000,
  })
}

export function useVerifyAuditChain(enabled: boolean) {
  return useQuery({
    queryKey: [...AUDIT_KEY, 'verify'],
    queryFn: async () => {
      const res = await api.get<ApiResponse<AuditVerifyResult>>('/audit-logs/verify?count=1000')
      return res.data.data
    },
    enabled,
    staleTime: 60_000,
  })
}
