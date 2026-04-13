import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'

const DATA_PORTABILITY_KEY = ['data-portability'] as const

export interface DataPortabilityResponse {
  job_id: string
  status: string
}

export function useRequestDataPortability() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (userID: string) => {
      const res = await api.post<ApiResponse<DataPortabilityResponse>>(
        `/compliance/data-portability/${userID}`,
        {},
      )
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['jobs'] })
      queryClient.invalidateQueries({ queryKey: DATA_PORTABILITY_KEY })
    },
  })
}
