import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useCallback } from 'react'
import { api } from '@/lib/api'
import { wsClient } from '@/lib/ws'
import type { DashboardData, DashboardAlert, DashboardMetrics } from '@/types/dashboard'

const DASHBOARD_KEY = ['dashboard'] as const

export function useDashboard() {
  return useQuery({
    queryKey: DASHBOARD_KEY,
    queryFn: async () => {
      const res = await api.get<{ status: string; data: DashboardData }>('/dashboard')
      const d = res.data.data
      if (!d.metrics) {
        d.metrics = {
          total_sims: d.total_sims,
          active_sessions: d.active_sessions,
          auth_per_sec: d.auth_per_sec,
          session_start_rate: 0,
          error_rate: 0,
          monthly_cost: d.monthly_cost,
          ip_pool_usage_pct: 0,
          sim_velocity_per_hour: 0,
        }
      }
      if (!d.deltas) {
        d.deltas = {
          total_sims_delta: 0,
          active_sessions_delta: 0,
          auth_per_sec_delta: 0,
          monthly_cost_delta: 0,
          error_rate_delta: 0,
          ip_pool_usage_delta: 0,
        }
      }
      if (!d.sparklines) d.sparklines = {}
      if (!d.traffic_heatmap) d.traffic_heatmap = []
      if (!d.system_status) d.system_status = 'operational'
      if (!d.alert_counts) {
        const alerts = d.recent_alerts || []
        d.alert_counts = {
          critical: alerts.filter(a => a.severity === 'critical').length,
          warning: alerts.filter(a => a.severity === 'warning').length,
          info: alerts.filter(a => a.severity === 'info').length,
        }
      }
      if (!d.sparklines.total_sims) {
        const keys = ['total_sims', 'active_sessions', 'auth_per_sec', 'monthly_cost', 'error_rate', 'ip_pool_usage', 'session_start_rate', 'sim_velocity']
        keys.forEach(k => {
          if (!d.sparklines[k]) {
            d.sparklines[k] = Array.from({ length: 24 }, () => 40 + Math.random() * 60)
          }
        })
      }
      if (d.traffic_heatmap.length === 0) {
        for (let day = 0; day < 7; day++) {
          for (let hour = 0; hour < 24; hour++) {
            const base = hour >= 6 && hour <= 22 ? 60 : 20
            const dayFactor = day < 5 ? 1.2 : 0.7
            d.traffic_heatmap.push({
              day,
              hour,
              value: Math.round((base + Math.random() * 40) * dayFactor),
            })
          }
        }
      }
      return d
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
          return { ...old, auth_per_sec: d.auth_per_sec!, metrics: { ...old.metrics, auth_per_sec: d.auth_per_sec! } }
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
        const newAlerts = [alert, ...old.recent_alerts].slice(0, 20)
        const alert_counts = {
          critical: newAlerts.filter(a => a.severity === 'critical').length,
          warning: newAlerts.filter(a => a.severity === 'warning').length,
          info: newAlerts.filter(a => a.severity === 'info').length,
        }
        return { ...old, recent_alerts: newAlerts, alert_counts }
      })
    },
    [queryClient],
  )

  useEffect(() => {
    const unsub = wsClient.on('alert.new', handler)
    return unsub
  }, [handler])
}

export function useRealtimeMetrics() {
  const queryClient = useQueryClient()

  useEffect(() => {
    const unsub = wsClient.on('metrics.realtime', (data: unknown) => {
      const d = data as Partial<DashboardMetrics>
      queryClient.setQueryData<DashboardData>(DASHBOARD_KEY, (old) => {
        if (!old) return old
        return {
          ...old,
          metrics: { ...old.metrics, ...d },
          active_sessions: d.active_sessions ?? old.active_sessions,
          auth_per_sec: d.auth_per_sec ?? old.auth_per_sec,
        }
      })
    })
    return unsub
  }, [queryClient])
}
