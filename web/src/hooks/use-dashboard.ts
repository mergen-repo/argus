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
      // Backend returns all metrics at the top level. The realtime WS
      // pusher pushes metrics.realtime events which can arrive BEFORE
      // /dashboard resolves and create a partial metrics object. So we
      // always merge the authoritative top-level values into metrics
      // rather than short-circuiting when metrics already exists.
      const raw = d as unknown as {
        ip_pool_usage_pct?: number
        session_start_rate?: number
        error_rate?: number
        sim_velocity_per_hour?: number
      }
      const topLevelMetrics = {
        total_sims: d.total_sims,
        active_sessions: d.active_sessions,
        auth_per_sec: d.auth_per_sec,
        session_start_rate: raw.session_start_rate ?? 0,
        error_rate: raw.error_rate ?? 0,
        monthly_cost: d.monthly_cost,
        ip_pool_usage_pct: raw.ip_pool_usage_pct ?? 0,
        sim_velocity_per_hour: raw.sim_velocity_per_hour ?? 0,
      }
      d.metrics = { ...(d.metrics ?? {}), ...topLevelMetrics }
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
            d.sparklines[k] = []
          }
        })
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
        // metrics.realtime payload carries GLOBAL counters (auth_per_sec,
        // error_rate, active_sessions). For tenant-scoped views we only
        // accept the rate metrics (auth_per_sec, error_rate, latency*).
        // active_sessions would reflect all tenants; keep the tenant-
        // scoped value from /dashboard instead.
        const { active_sessions: _globalSessions, ...rateOnly } = d
        return {
          ...old,
          metrics: { ...old.metrics, ...rateOnly },
          auth_per_sec: d.auth_per_sec ?? old.auth_per_sec,
        }
      })
    })
    return unsub
  }, [queryClient])
}
