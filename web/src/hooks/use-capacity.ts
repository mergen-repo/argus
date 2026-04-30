import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'

export interface CapacityPoolForecast {
  id: string
  name: string
  cidr: string
  total: number
  used: number
  available: number
  utilization_pct: number
  allocation_rate: number
  exhaustion_hours: number | null
}

export interface CapacityResponse {
  total_sims: number
  active_sessions: number
  auth_per_sec: number
  sim_capacity: number
  session_capacity: number
  auth_capacity: number
  monthly_growth_sims: number
  ip_pools: CapacityPoolForecast[]
}

export function useCapacity() {
  return useQuery({
    queryKey: ['capacity'],
    queryFn: async () => {
      const res = await api.get<ApiResponse<CapacityResponse>>('/system/capacity')
      return res.data.data
    },
    staleTime: 60_000,
  })
}
