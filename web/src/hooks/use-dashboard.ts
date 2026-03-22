import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useCallback } from 'react'
import { api } from '@/lib/api'
import { wsClient } from '@/lib/ws'
import type { DashboardData, DashboardAlert } from '@/types/dashboard'

const DASHBOARD_KEY = ['dashboard'] as const

export function useDashboard() {
  return useQuery({
    queryKey: DASHBOARD_KEY,
    queryFn: async () => {
      const res = await api.get<{ status: string; data: DashboardData }>('/dashboard')
      return res.data.data
    },
    staleTime: 30_000,
    refetchInterval: 30_000,
  })
}

export function useRealtimeAuthPerSec() {
  const queryClient = useQueryClient()
  const latestRef = useRef<number>(0)

  useEffect(() => {
    const unsub = wsClient.on('metrics.realtime', (data: unknown) => {
      const d = data as { auth_per_sec?: number }
      if (d.auth_per_sec !== undefined) {
        latestRef.current = d.auth_per_sec
        queryClient.setQueryData<DashboardData>(DASHBOARD_KEY, (old) => {
          if (!old) return old
          return { ...old, auth_per_sec: d.auth_per_sec! }
        })
      }
    })
    return unsub
  }, [queryClient])

  return latestRef
}

export function useRealtimeAlerts() {
  const queryClient = useQueryClient()

  const handler = useCallback(
    (data: unknown) => {
      const alert = data as DashboardAlert
      if (!alert.id) return
      queryClient.setQueryData<DashboardData>(DASHBOARD_KEY, (old) => {
        if (!old) return old
        const newAlerts = [alert, ...old.recent_alerts].slice(0, 10)
        return { ...old, recent_alerts: newAlerts }
      })
    },
    [queryClient],
  )

  useEffect(() => {
    const unsub = wsClient.on('alert.new', handler)
    return unsub
  }, [handler])
}
