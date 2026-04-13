import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'
import type { PolicyViolation } from '@/components/shared'

export function useViolation(id: string | undefined) {
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

export function useRemediate(violationId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ action, reason }: { action: 'suspend_sim' | 'escalate' | 'dismiss'; reason: string }) => {
      const res = await api.post<ApiResponse<{ violation: PolicyViolation; sim?: unknown }>>(
        `/policy-violations/${violationId}/remediate`,
        { action, reason },
      )
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['violations', 'detail', violationId] })
      queryClient.invalidateQueries({ queryKey: ['violations'] })
    },
  })
}
