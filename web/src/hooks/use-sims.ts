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
  SIMUsageData,
  SIMCompareResult,
  SIMCDR,
  ListResponse,
  ApiResponse,
  BulkJobResponse,
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
  if (filters.policy_version_id) params.set('policy_version_id', filters.policy_version_id)
  if (filters.policy_id) params.set('policy_id', filters.policy_id)
  if (filters.rollout_id) params.set('rollout_id', filters.rollout_id)
  if (filters.rollout_stage_pct) params.set('rollout_stage_pct', String(filters.rollout_stage_pct))
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

export function useSIMSessions(simId: string, state?: string) {
  return useInfiniteQuery({
    queryKey: [...SIMS_KEY, 'sessions', simId, state],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '50')
      if (state) params.set('state', state)
      const res = await api.get<ListResponse<SIMSession>>(
        `/sims/${simId}/sessions?${params.toString()}`,
      )
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    enabled: !!simId,
  })
}

export function useSIMUsage(simId: string, period: string = '30d') {
  return useQuery({
    queryKey: [...SIMS_KEY, 'usage', simId, period],
    queryFn: async () => {
      const params = new URLSearchParams()
      params.set('period', period)
      const res = await api.get<ApiResponse<SIMUsageData>>(
        `/sims/${simId}/usage?${params.toString()}`,
      )
      return res.data.data
    },
    enabled: !!simId,
    staleTime: 60_000,
  })
}

function periodToRange(period: string): { from: string; to: string } {
  const now = Date.now()
  const offsets: Record<string, number> = {
    '24h': 24 * 60 * 60 * 1000,
    '7d': 7 * 24 * 60 * 60 * 1000,
    '30d': 30 * 24 * 60 * 60 * 1000,
    '90d': 90 * 24 * 60 * 60 * 1000,
  }
  const ms = offsets[period] ?? offsets['30d']
  return { from: new Date(now - ms).toISOString(), to: new Date(now).toISOString() }
}

export function useSIMCDRs(simId: string, period: string = '30d') {
  return useInfiniteQuery({
    queryKey: [...SIMS_KEY, 'cdrs', simId, period],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '20')
      params.set('sim_id', simId)
      const { from, to } = periodToRange(period)
      params.set('from', from)
      params.set('to', to)
      const res = await api.get<ListResponse<SIMCDR>>(`/cdrs?${params.toString()}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
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

export interface SIMCurrentIP {
  allocated: boolean
  ip_id?: string
  address_v4?: string | null
  address_v6?: string | null
  state?: string
  allocation_type?: string
  allocated_at?: string | null
  pool_id?: string
  pool_name?: string
  pool_cidr_v4?: string
}

export function useSIMCurrentIP(simId: string) {
  return useQuery({
    queryKey: ['sims', 'ip-current', simId],
    queryFn: async () => {
      const res = await api.get<ApiResponse<SIMCurrentIP>>(`/sims/${simId}/ip-current`)
      return res.data.data
    },
    enabled: !!simId,
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
      const res = await api.post<ApiResponse<SIM> & { meta?: { undo_action_id?: string } }>(
        `/sims/${simId}/${action}`,
        reason ? { reason } : {},
      )
      return {
        sim: res.data.data,
        undoActionId: res.data.meta?.undo_action_id,
      }
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
      segmentId,
      targetState,
      reason,
    }: {
      simIds?: string[]
      segmentId?: string
      targetState: string
      reason?: string
    }) => {
      const res = await api.post<ApiResponse<BulkJobResponse>>('/sims/bulk/state-change', {
        ...(segmentId ? { segment_id: segmentId } : { sim_ids: simIds }),
        target_state: targetState,
        reason,
      })
      return res.data.data
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
      segmentId,
      policyVersionId,
    }: {
      simIds?: string[]
      segmentId?: string
      policyVersionId: string
    }) => {
      const res = await api.post<ApiResponse<BulkJobResponse>>('/sims/bulk/policy-assign', {
        ...(segmentId ? { segment_id: segmentId } : { sim_ids: simIds }),
        policy_version_id: policyVersionId,
      })
      return res.data.data
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
      return res.data.data as { job_id: string; tenant_id: string; status: string }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: SIMS_KEY })
    },
  })
}

export function useSIMComparePair(idA: string, idB: string) {
  return useQuery({
    queryKey: [...SIMS_KEY, 'compare', idA, idB],
    queryFn: async () => {
      const res = await api.post<ApiResponse<SIMCompareResult>>('/sims/compare', { sim_id_a: idA, sim_id_b: idB })
      return res.data.data
    },
    enabled: !!idA && !!idB && idA !== idB,
    staleTime: 10_000,
  })
}
