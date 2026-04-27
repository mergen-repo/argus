// FIX-244 DEV-528: single source of truth for violation queries + mutations.
//
// Previously split between an inline `useAcknowledgeViolation` in
// pages/violations/index.tsx and `useRemediate` in use-violation-detail.ts —
// each invalidating a different (and incomplete) set of TanStack Query keys.
// PAT-006 RECURRENCE hot zone (sibling-hook key drift). Consolidating here
// behind one `invalidateViolations(qc, opts)` helper guarantees every
// mutation hits the same five caches.

import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse, ListResponse } from '@/types/sim'
import type { PolicyViolation } from '@/types/violation'

export interface ViolationFilters {
  violation_type?: string
  action_taken?: string
  severity?: string
  status?: string
  date_from?: string
  date_to?: string
  sim_id?: string
  policy_id?: string
}

interface BulkResult {
  succeeded: string[]
  failed: { id: string; error_code: string; message: string }[]
}

interface InvalidateOptions {
  /** Specific violation id to invalidate the detail query for. */
  id?: string
  /** SIM id to invalidate when the mutation also moves SIM state (suspend). */
  simId?: string
}

/**
 * Invalidate every cache that a violation mutation can affect.
 *
 *   ['violations']                     — paginated list
 *   ['violations', 'counts']           — chart counts
 *   ['violations', 'detail', id]       — detail panel (when id provided)
 *   ['policy-violations', 'related']   — RelatedViolationsTab
 *   ['sims'] / ['sims', simId]         — suspend_sim flips SIM state
 *   ['audit-logs']                     — every action emits an audit row
 */
export function invalidateViolations(qc: QueryClient, opts: InvalidateOptions = {}): void {
  qc.invalidateQueries({ queryKey: ['violations'] })
  qc.invalidateQueries({ queryKey: ['policy-violations', 'related'] })
  qc.invalidateQueries({ queryKey: ['audit-logs'] })
  if (opts.id) {
    qc.invalidateQueries({ queryKey: ['violations', 'detail', opts.id] })
  }
  if (opts.simId) {
    qc.invalidateQueries({ queryKey: ['sims'] })
    qc.invalidateQueries({ queryKey: ['sims', opts.simId] })
  }
}

function buildViolationListParams(filters: ViolationFilters, cursor?: string, limit = 50): URLSearchParams {
  const params = new URLSearchParams()
  if (cursor) params.set('cursor', cursor)
  params.set('limit', String(limit))
  if (filters.violation_type) params.set('violation_type', filters.violation_type)
  if (filters.action_taken) params.set('action_taken', filters.action_taken)
  if (filters.severity) params.set('severity', filters.severity)
  if (filters.status) params.set('status', filters.status)
  if (filters.date_from) params.set('date_from', filters.date_from)
  if (filters.date_to) params.set('date_to', filters.date_to)
  if (filters.sim_id) params.set('sim_id', filters.sim_id)
  if (filters.policy_id) params.set('policy_id', filters.policy_id)
  return params
}

export function useViolations(filters: ViolationFilters) {
  return useInfiniteQuery({
    queryKey: ['violations', filters],
    queryFn: async ({ pageParam }) => {
      const params = buildViolationListParams(filters, (pageParam as string) || undefined)
      const res = await api.get<ListResponse<PolicyViolation>>(`/policy-violations?${params.toString()}`)
      return { ...res.data, data: res.data.data ?? [] }
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) => (lastPage.meta?.has_more ? lastPage.meta.cursor : undefined),
    staleTime: 15_000,
  })
}

export function useViolationCounts() {
  return useQuery({
    queryKey: ['violations', 'counts'],
    queryFn: async () => {
      const res = await api.get<{ status: string; data: Record<string, number> }>('/policy-violations/counts')
      return res.data.data ?? {}
    },
    staleTime: 30_000,
  })
}

export function useViolationDetail(id: string | undefined) {
  return useQuery({
    queryKey: ['violations', 'detail', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<PolicyViolation>>(`/policy-violations/${id}`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 30_000,
    retry: (failureCount, error: unknown) => {
      const err = error as { status?: number }
      if (err?.status === 404) return false
      return failureCount < 2
    },
  })
}

interface AcknowledgeResult {
  id: string
  acknowledged_at: string
  acknowledged_by: string
  note?: string
}

export function useAcknowledgeViolation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, note }: { id: string; note?: string }) => {
      const res = await api.post<ApiResponse<AcknowledgeResult>>(
        `/policy-violations/${id}/acknowledge`,
        { note },
      )
      return res.data.data
    },
    onSuccess: (_data, vars) => {
      invalidateViolations(qc, { id: vars.id })
    },
  })
}

export type RemediateAction = 'suspend_sim' | 'escalate' | 'dismiss'

interface RemediateVars {
  violationId: string
  action: RemediateAction
  reason: string
  /** Optional SIM id for cache invalidation when action = suspend_sim. */
  simId?: string
}

/**
 * Mutation hook for the per-violation Remediate endpoint. The violationId is
 * supplied at call time (not at hook creation) so a single hook instance can
 * service every row in a list. Pass `simId` for `suspend_sim` actions to
 * invalidate the SIM-specific caches as well.
 */
export function useRemediate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ violationId, action, reason, simId }: RemediateVars) => {
      const res = await api.post<ApiResponse<{ violation: PolicyViolation; sim?: unknown }>>(
        `/policy-violations/${violationId}/remediate`,
        { action, reason },
      )
      return { data: res.data.data, violationId, simId, action }
    },
    onSuccess: (result) => {
      invalidateViolations(qc, {
        id: result.violationId,
        simId: result.action === 'suspend_sim' ? result.simId : undefined,
      })
    },
  })
}

export function useBulkAcknowledge() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ ids, note }: { ids: string[]; note?: string }) => {
      const res = await api.post<ApiResponse<BulkResult>>(
        '/policy-violations/bulk/acknowledge',
        { ids, note },
      )
      return res.data.data
    },
    onSuccess: () => {
      invalidateViolations(qc)
    },
  })
}

export function useBulkDismiss() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ ids, reason }: { ids: string[]; reason: string }) => {
      const res = await api.post<ApiResponse<BulkResult>>(
        '/policy-violations/bulk/dismiss',
        { ids, reason },
      )
      return res.data.data
    },
    onSuccess: () => {
      invalidateViolations(qc)
    },
  })
}
