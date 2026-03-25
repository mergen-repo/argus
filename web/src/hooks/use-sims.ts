import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type {
  SIM,
  SIMHistoryEntry,
  SIMSession,
  SIMSegment,
  SegmentCount,
  DiagnosticResult,
  SIMListFilters,
  ListResponse,
  ApiResponse,
} from '@/types/sim'

const SIMS_KEY = ['sims'] as const
const SEGMENTS_KEY = ['sim-segments'] as const

function buildListParams(filters: SIMListFilters, cursor?: string) {
  const params = new URLSearchParams()
  if (cursor) params.set('cursor', cursor)
  params.set('limit', '50')
  if (filters.state) params.set('state', filters.state)
  if (filters.operator_id) params.set('operator_id', filters.operator_id)
  if (filters.apn_id) params.set('apn_id', filters.apn_id)
  if (filters.rat_type) params.set('rat_type', filters.rat_type)
  if (filters.q) params.set('q', filters.q)
  if (filters.iccid) params.set('iccid', filters.iccid)
  if (filters.imsi) params.set('imsi', filters.imsi)
  if (filters.msisdn) params.set('msisdn', filters.msisdn)
  if (filters.ip) params.set('ip', filters.ip)
  return params.toString()
}

export function useSIMList(filters: SIMListFilters) {
  return useInfiniteQuery({
    queryKey: [...SIMS_KEY, 'list', filters],
    queryFn: async ({ pageParam }) => {
      const qs = buildListParams(filters, pageParam as string | undefined)
      const res = await api.get<ListResponse<SIM>>(`/sims?${qs}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    staleTime: 15_000,
  })
}

export function useSIM(id: string) {
  return useQuery({
    queryKey: [...SIMS_KEY, 'detail', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<SIM>>(`/sims/${id}`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 10_000,
  })
}

export function useSIMHistory(simId: string) {
  return useInfiniteQuery({
    queryKey: [...SIMS_KEY, 'history', simId],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '50')
      const res = await api.get<ListResponse<SIMHistoryEntry>>(
        `/sims/${simId}/history?${params.toString()}`,
      )
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    enabled: !!simId,
  })
}

export function useSIMSessions(simId: string) {
  return useInfiniteQuery({
    queryKey: [...SIMS_KEY, 'sessions', simId],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '50')
      params.set('sim_id', simId)
      const res = await api.get<ListResponse<SIMSession>>(
        `/sessions?${params.toString()}`,
      )
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    enabled: !!simId,
  })
}

export function useSIMUsage(simId: string) {
  return useQuery({
    queryKey: [...SIMS_KEY, 'usage', simId],
    queryFn: async () => {
      const res = await api.get<ApiResponse<unknown>>(`/sims/${simId}/usage`)
      return res.data.data
    },
    enabled: !!simId,
    staleTime: 60_000,
  })
}

export function useSIMDiagnostics(simId: string) {
  return useMutation({
    mutationFn: async (includeTestAuth: boolean = false) => {
      const res = await api.post<ApiResponse<DiagnosticResult>>(
        `/sims/${simId}/diagnose`,
        { include_test_auth: includeTestAuth },
      )
      return res.data.data
    },
  })
}

export function useSegments() {
  return useQuery({
    queryKey: SEGMENTS_KEY,
    queryFn: async () => {
      const res = await api.get<ListResponse<SIMSegment>>('/sim-segments?limit=100')
      return res.data.data
    },
    staleTime: 60_000,
  })
}

export function useSegmentCount(segmentId: string) {
  return useQuery({
    queryKey: [...SEGMENTS_KEY, 'count', segmentId],
    queryFn: async () => {
      const res = await api.get<ApiResponse<SegmentCount>>(
        `/sim-segments/${segmentId}/count`,
      )
      return res.data.data
    },
    enabled: !!segmentId,
    staleTime: 30_000,
  })
}

export function useSIMStateAction() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      simId,
      action,
      reason,
    }: {
      simId: string
      action: 'activate' | 'suspend' | 'resume' | 'terminate' | 'report-lost'
      reason?: string
    }) => {
      const res = await api.post<ApiResponse<SIM>>(
        `/sims/${simId}/${action}`,
        reason ? { reason } : {},
      )
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: SIMS_KEY })
    },
  })
}

export function useBulkStateChange() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      simIds,
      targetState,
      reason,
    }: {
      simIds: string[]
      targetState: string
      reason?: string
    }) => {
      const res = await api.post('/sims/bulk/state-change', {
        sim_ids: simIds,
        target_state: targetState,
        reason,
      })
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: SIMS_KEY })
    },
  })
}

export function useBulkPolicyAssign() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      simIds,
      policyId,
    }: {
      simIds: string[]
      policyId: string
    }) => {
      const res = await api.post('/sims/bulk/policy-assign', {
        sim_ids: simIds,
        policy_id: policyId,
      })
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: SIMS_KEY })
    },
  })
}

export function useImportSIMs() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ file, reserveStaticIP }: { file: File; reserveStaticIP: boolean }) => {
      const formData = new FormData()
      formData.append('file', file)
      if (reserveStaticIP) formData.append('reserve_static_ip', 'true')
      const res = await api.post('/sims/bulk/import', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
      })
      return res.data.data as { job_id: string; rows_parsed: number; errors: string[] }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: SIMS_KEY })
    },
  })
}
