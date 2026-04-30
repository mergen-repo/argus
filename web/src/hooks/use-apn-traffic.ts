import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'

export interface APNTrafficBucket {
  ts: string
  bytes_in: number
  bytes_out: number
  auth_count: number
}

export interface APNTrafficResponse {
  period: string
  series: APNTrafficBucket[]
}

export function useAPNTraffic(apnId: string, period: string) {
  return useQuery({
    queryKey: ['apns', 'traffic', apnId, period],
    queryFn: async () => {
      const res = await api.get<ApiResponse<APNTrafficResponse>>(`/apns/${apnId}/traffic?period=${period}`)
      return res.data.data
    },
    enabled: !!apnId,
    staleTime: 60_000,
  })
}
