export interface SIMByState {
  state: string
  count: number
}

export interface OperatorHealth {
  id: string
  name: string
  status: 'healthy' | 'degraded' | 'down'
  health_pct: number
}

export interface TopAPN {
  id: string
  name: string
  session_count: number
}

export interface DashboardAlert {
  id: string
  type: string
  severity: 'critical' | 'warning' | 'info'
  state: string
  message: string
  detected_at: string
}

export interface DashboardData {
  total_sims: number
  active_sessions: number
  auth_per_sec: number
  monthly_cost: number
  sim_by_state: SIMByState[]
  operator_health: OperatorHealth[]
  top_apns: TopAPN[]
  recent_alerts: DashboardAlert[]
}
