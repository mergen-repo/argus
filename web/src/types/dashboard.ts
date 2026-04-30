export interface SIMByState {
  state: string
  count: number
}

export interface OperatorHealth {
  id: string
  name: string
  status: 'healthy' | 'degraded' | 'down'
  health_pct: number
  code?: string
  sla_target?: number
  active_sessions?: number
  last_health_check?: string
  latency_ms?: number
  auth_rate?: number
  latency_sparkline?: number[]
  sla_latency_ms?: number
}

export interface TopAPN {
  id: string
  name: string
  session_count: number
  bytes_total: number
}

export type AlertSource = 'sim' | 'operator' | 'infra' | 'policy' | 'system'

export interface DashboardAlert {
  id: string
  type: string
  severity: 'critical' | 'warning' | 'info'
  source: AlertSource
  state: string
  message: string
  detected_at: string
  sim_id?: string
  operator_id?: string
  apn_id?: string
  meta?: Record<string, unknown>
}

export interface DashboardMetrics {
  total_sims: number
  active_sessions: number
  auth_per_sec: number
  session_start_rate: number
  error_rate: number
  monthly_cost: number
  ip_pool_usage_pct: number
  sim_velocity_per_hour: number
}

export interface MetricDelta {
  total_sims_delta: number
  active_sessions_delta: number
  auth_per_sec_delta: number
  monthly_cost_delta: number
  error_rate_delta: number
  ip_pool_usage_delta: number
}

export interface TrafficHeatmapCell {
  day: number
  hour: number
  value: number
  raw_bytes: number
}

export interface TopIPPool {
  id: string
  name: string
  usage_pct: number
}

export interface DashboardData {
  total_sims: number
  active_sessions: number
  auth_per_sec: number
  monthly_cost: number
  metrics: DashboardMetrics
  deltas: MetricDelta
  sim_by_state: SIMByState[]
  operator_health: OperatorHealth[]
  top_apns: TopAPN[]
  recent_alerts: DashboardAlert[]
  traffic_heatmap: TrafficHeatmapCell[]
  sparklines: Record<string, number[]>
  system_status: 'operational' | 'degraded' | 'critical'
  alert_counts: { critical: number; warning: number; info: number }
  top_ip_pool?: TopIPPool | null
}
