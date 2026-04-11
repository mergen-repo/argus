import { useInfiniteQuery, useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ESimProfile, ESimSwitchResult, ESimCreateRequest } from '@/types/esim'
import type { ListResponse, ApiResponse } from '@/types/sim'

const ESIM_KEY = ['esim-profiles'] as const

export function useESimList(filters: { operator_id?: string; state?: string }) {
  return useInfiniteQuery({
    queryKey: [...ESIM_KEY, 'list', filters],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '50')
      if (filters.operator_id) params.set('operator_id', filters.operator_id)
      if (filters.state) params.set('state', filters.state)
      const res = await api.get<ListResponse<ESimProfile>>(`/esim-profiles?${params.toString()}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    staleTime: 15_000,
  })
}

export function useEnableProfile() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (profileId: string) => {
      const res = await api.post<ApiResponse<ESimProfile>>(`/esim-profiles/${profileId}/enable`)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ESIM_KEY })
    },
  })
}

export function useDisableProfile() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (profileId: string) => {
      const res = await api.post<ApiResponse<ESimProfile>>(`/esim-profiles/${profileId}/disable`)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ESIM_KEY })
    },
  })
}

export function useSwitchProfile() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ profileId, targetProfileId }: { profileId: string; targetProfileId: string }) => {
      const res = await api.post<ApiResponse<ESimSwitchResult>>(`/esim-profiles/${profileId}/switch`, {
        target_profile_id: targetProfileId,
      })
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ESIM_KEY })
    },
  })
}

export function useESimListBySim(simId: string) {
  return useQuery({
    queryKey: [...ESIM_KEY, 'by-sim', simId],
    queryFn: async () => {
      const res = await api.get<ListResponse<ESimProfile>>(`/esim-profiles?sim_id=${simId}&limit=50`)
      return res.data.data
    },
    enabled: !!simId,
    staleTime: 15_000,
  })
}

export function useCreateProfile() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: ESimCreateRequest) => {
      const res = await api.post<ApiResponse<ESimProfile>>('/esim-profiles', body)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ESIM_KEY })
    },
  })
}

export function useDeleteProfile() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (profileId: string) => {
      const res = await api.delete<ApiResponse<ESimProfile>>(`/esim-profiles/${profileId}`)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ESIM_KEY })
    },
  })
}
