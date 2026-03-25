export interface SIMByState {
  state: string
  count: number
}

export interface OperatorHealth {
  id: string
  name: string
  code: string
  status: 'healthy' | 'degraded' | 'down'
  health_pct: number
  latency_ms: number
  sla_target: number
  active_sessions: number
  auth_rate: number
  last_check: string
}

export interface TopAPN {
  id: string
  name: string
  session_count: number
  bytes_total: number
}

export interface DashboardAlert {
  id: string
  type: string
  severity: 'critical' | 'warning' | 'info'
  state: string
  message: string
  entity_type?: string
  entity_id?: string
  detected_at: string
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
}
