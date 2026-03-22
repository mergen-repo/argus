export interface Operator {
  id: string
  name: string
  code: string
  mcc: string
  mnc: string
  adapter_type: string
  supported_rat_types: string[]
  health_status: string
  health_check_interval_sec: number
  failover_policy: string
  failover_timeout_ms: number
  circuit_breaker_threshold: number
  circuit_breaker_recovery_sec: number
  sla_uptime_target?: number
  state: string
  created_at: string
  updated_at: string
}

export interface OperatorHealthDetail {
  health_status: string
  latency_ms?: number
  circuit_state: string
  last_check?: string
  uptime_24h: number
  failure_count: number
}

export interface OperatorTestResult {
  success: boolean
  latency_ms: number
  error?: string
}
