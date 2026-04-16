import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type {
  Policy,
  PolicyListItem,
  PolicyVersion,
  DryRunResult,
  DiffResponse,
  PolicyRollout,
  ListResponse,
  ApiResponse,
} from '@/types/policy'

const POLICIES_KEY = ['policies'] as const

export function usePolicyList(search?: string, status?: string) {
  return useInfiniteQuery({
    queryKey: [...POLICIES_KEY, 'list', search, status],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '50')
      if (search) params.set('q', search)
      if (status) params.set('status', status)
      const res = await api.get<ListResponse<PolicyListItem>>(`/policies?${params.toString()}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    staleTime: 15_000,
  })
}

export function usePolicy(id: string) {
  return useQuery({
    queryKey: [...POLICIES_KEY, 'detail', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<Policy>>(`/policies/${id}`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 10_000,
  })
}

export function useCreatePolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: { name: string; description?: string; scope: string; scope_ref_id?: string; dsl_source: string }) => {
      const res = await api.post<ApiResponse<Policy>>('/policies', data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: POLICIES_KEY })
    },
  })
}

export function useUpdatePolicy(id: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: { name?: string; description?: string; state?: string }) => {
      const res = await api.patch<ApiResponse<Policy>>(`/policies/${id}`, data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: POLICIES_KEY })
    },
  })
}

export function useDeletePolicy() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      const res = await api.delete<{ status: string; data: { deleted: boolean }; meta?: { undo_action_id?: string } }>(`/policies/${id}`)
      return { undoActionId: res.data.meta?.undo_action_id }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: POLICIES_KEY })
    },
  })
}

export function useCreateVersion(policyId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (data: { dsl_source: string; clone_from_version_id?: string }) => {
      const res = await api.post<ApiResponse<PolicyVersion>>(`/policies/${policyId}/versions`, data)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...POLICIES_KEY, 'detail', policyId] })
    },
  })
}

export function useUpdateVersion() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ versionId, dsl_source }: { versionId: string; dsl_source: string }) => {
      const res = await api.patch<ApiResponse<PolicyVersion>>(`/policy-versions/${versionId}`, { dsl_source })
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: POLICIES_KEY })
    },
  })
}

export function useActivateVersion(policyId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (versionId: string) => {
      const res = await api.post<ApiResponse<PolicyVersion>>(`/policy-versions/${versionId}/activate`)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...POLICIES_KEY, 'detail', policyId] })
    },
  })
}

export function useDryRun(versionId: string | undefined) {
  return useQuery({
    queryKey: [...POLICIES_KEY, 'dry-run', versionId],
    queryFn: async () => {
      const res = await api.post<ApiResponse<DryRunResult>>(`/policy-versions/${versionId}/dry-run`)
      return res.data.data
    },
    enabled: !!versionId,
    staleTime: 0,
    refetchOnWindowFocus: false,
  })
}

export function useDryRunMutation() {
  return useMutation({
    mutationFn: async (versionId: string) => {
      const res = await api.post<ApiResponse<DryRunResult>>(`/policy-versions/${versionId}/dry-run`)
      return res.data.data
    },
  })
}

export function useVersionDiff(id1: string | undefined, id2: string | undefined) {
  return useQuery({
    queryKey: [...POLICIES_KEY, 'diff', id1, id2],
    queryFn: async () => {
      const res = await api.get<ApiResponse<DiffResponse>>(`/policy-versions/${id1}/diff/${id2}`)
      return res.data.data
    },
    enabled: !!id1 && !!id2,
    staleTime: 60_000,
  })
}

export function useStartRollout(policyId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ versionId, stages }: { versionId: string; stages: number[] }) => {
      const res = await api.post<ApiResponse<PolicyRollout>>(`/policy-versions/${versionId}/rollout`, { stages })
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...POLICIES_KEY, 'detail', policyId] })
      queryClient.invalidateQueries({ queryKey: [...POLICIES_KEY, 'rollout'] })
    },
  })
}

export function useRollout(rolloutId: string | undefined) {
  return useQuery({
    queryKey: [...POLICIES_KEY, 'rollout', rolloutId],
    queryFn: async () => {
      const res = await api.get<ApiResponse<PolicyRollout>>(`/policy-rollouts/${rolloutId}`)
      return res.data.data
    },
    enabled: !!rolloutId,
    staleTime: 5_000,
    refetchInterval: 10_000,
  })
}

export function useAdvanceRollout() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (rolloutId: string) => {
      const res = await api.post<ApiResponse<PolicyRollout>>(`/policy-rollouts/${rolloutId}/advance`)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...POLICIES_KEY, 'rollout'] })
    },
  })
}

export function useRollbackRollout() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (rolloutId: string) => {
      const res = await api.post<ApiResponse<PolicyRollout>>(`/policy-rollouts/${rolloutId}/rollback`)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...POLICIES_KEY, 'rollout'] })
      queryClient.invalidateQueries({ queryKey: POLICIES_KEY })
    },
  })
}
