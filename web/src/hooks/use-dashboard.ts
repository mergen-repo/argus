import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useCallback } from 'react'
import { api } from '@/lib/api'
import { wsClient } from '@/lib/ws'
import type { DashboardData, DashboardAlert, DashboardMetrics, OperatorHealth } from '@/types/dashboard'

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

// useRealtimeActiveSessions live-updates the Active Sessions KPI card
// by mutating the dashboard query cache on session lifecycle events.
// The global metrics.realtime pusher reports sum-across-tenants which
// we don't want; instead we derive the delta from tenant-scoped WS
// events (BroadcastToTenant already filters to the admin's tenant).
// session.started → +1, session.ended → -1.
export function useRealtimeActiveSessions() {
  const queryClient = useQueryClient()

  useEffect(() => {
    const apply = (delta: number) => {
      queryClient.setQueryData<DashboardData>(DASHBOARD_KEY, (old) => {
        if (!old) return old
        const next = Math.max(0, (old.active_sessions ?? 0) + delta)
        return {
          ...old,
          active_sessions: next,
          metrics: { ...old.metrics, active_sessions: next },
        }
      })
    }
    const unsubStart = wsClient.on('session.started', () => apply(1))
    const unsubEnd = wsClient.on('session.ended', () => apply(-1))
    return () => { unsubStart(); unsubEnd() }
  }, [queryClient])
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

/**
 * Subscribes to `operator.health_changed` WS events and patches the operator-health list
 * in the DashboardData cache without a full refetch.
 *
 * Event source: backend operator health worker publishes when status flips OR latency
 * delta >10% (see FIX-203 plan). Hub relays `argus.events.operator.health` → client event
 * `operator.health_changed`.
 *
 * Orphan protection: if the event carries an operator_id not in the current
 * operator_health list, the .map() is a no-op (no new row added — the dashboard data
 * is authoritatively set by the /dashboard fetch + 30s refetch fallback).
 */
export function useRealtimeOperatorHealth() {
  const queryClient = useQueryClient();
  useEffect(() => {
    const handler = (data: unknown) => {
      const d = data as { operator_id?: string; current_status?: string; latency_ms?: number; timestamp?: string }
      if (!d || typeof d.operator_id !== 'string') return;
      queryClient.setQueryData<DashboardData>(DASHBOARD_KEY, (old) => {
        if (!old) return old;
        const next = old.operator_health.map((op) =>
          op.id === d.operator_id
            ? {
                ...op,
                status: (d.current_status as OperatorHealth['status']) ?? op.status,
                latency_ms: typeof d.latency_ms === 'number' ? d.latency_ms : op.latency_ms,
                last_health_check: d.timestamp ?? op.last_health_check,
                health_pct: statusToPct(d.current_status),
              }
            : op,
        );
        return { ...old, operator_health: next };
      });
    };
    const unsubscribe = wsClient.on('operator.health_changed', handler);
    return unsubscribe;
  }, [queryClient]);
}

function statusToPct(s: string | undefined): number {
  switch (s) {
    case 'healthy': return 99.9;
    case 'degraded': return 95;
    case 'down': return 0;
    default: return 0;
  }
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
