export interface KillSwitch {
  key: string
  label: string
  description: string
  enabled: boolean
  reason: string
  toggled_by: string | null
  toggled_at: string | null
  created_at: string
}

export interface ToggleKillSwitchRequest {
  enabled: boolean
  reason?: string
}

export interface MaintenanceWindow {
  id: string
  tenant_id: string | null
  title: string
  description: string
  starts_at: string
  ends_at: string
  affected_services: string[]
  cron_expression: string
  notify_plan: Record<string, unknown>
  state: string
  created_by: string | null
  created_at: string
}

export interface CreateMaintenanceWindowRequest {
  title: string
  description?: string
  starts_at: string
  ends_at: string
  affected_services?: string[]
  cron_expression?: string
  notify_plan?: Record<string, unknown>
}

export interface ChannelHealth {
  success_rate: number
  failure_rate: number
  retry_depth: number
  last_delivery_at: string | null
  p50_ms: number
  p95_ms: number
  p99_ms: number
  health: 'green' | 'yellow' | 'red'
}

export interface DeliveryStatus {
  webhook: ChannelHealth
  email: ChannelHealth
  sms: ChannelHealth
  in_app: ChannelHealth
  telegram: ChannelHealth
}

export interface TenantResourceItem {
  tenant_id: string
  tenant_name: string
  sim_count: number
  active_sessions: number
  api_rps: number
  cdr_bytes_30d: number
  storage_bytes: number
  spark: number[]
}

export interface QuotaMetric {
  current: number
  max: number
  pct: number
  status: 'ok' | 'warning' | 'danger'
}

export interface TenantQuota {
  tenant_id: string
  tenant_name: string
  sims: QuotaMetric
  api_rps: QuotaMetric
  sessions: QuotaMetric
  storage_bytes: QuotaMetric
}

export interface CostTenant {
  tenant_id: string
  tenant_name: string
  currency: string
  total: number
  radius_cost: number
  operator_cost: number
  sms_cost: number
  storage_cost: number
  trend: number[]
}

export interface ActiveSession {
  session_id: string
  user_id: string
  user_email: string
  tenant_id: string
  tenant_name: string
  ip_address: string
  browser: string
  os: string
  idle_seconds: number
  created_at: string
  last_seen_at: string
}

export interface SessionFilters {
  tenant_id?: string
  cursor?: string
  limit?: number
}

export interface APIKeyUsageItem {
  key_id: string
  key_name: string
  tenant_id: string
  tenant_name: string
  requests: number
  rate_limit: number
  consumption_pct: number
  error_rate: number
  anomaly: boolean
}

export interface DSARQueueItem {
  job_id: string
  type: string
  tenant_id: string
  subject_id: string
  status: string
  sla_hours: number
  sla_remaining_hours: number
  created_at: string
  updated_at: string
}

export interface DSARFilters {
  status?: string
  cursor?: string
  limit?: number
}

export interface PurgeHistoryItem {
  sim_id: string
  iccid: string
  msisdn: string
  tenant_id: string
  tenant_name: string
  purged_at: string
  reason: string
  actor_id: string | null
  actor_email?: string
  actor_name?: string
}

export interface PurgeHistoryFilters {
  tenant_id?: string
  from?: string
  to?: string
  cursor?: string
  limit?: number
}

// FIX-246: Tenant Usage dashboard
export type TenantPlan = 'starter' | 'standard' | 'enterprise'
export type TenantState = 'active' | 'suspended' | 'trial'

export interface TenantUsageMetric {
  current: number
  max: number
  pct: number
  status: 'ok' | 'warning' | 'critical'
}

export interface TenantUsageItem {
  tenant_id: string
  tenant_name: string
  plan: TenantPlan
  state: TenantState
  sims: TenantUsageMetric
  sessions: TenantUsageMetric
  api_rps: TenantUsageMetric
  storage_bytes: TenantUsageMetric
  user_count: number
  cdr_bytes_30d: number
  open_breach_count: number
}

export interface UsageTrendPoint {
  date: string
  sims: number
  sessions: number
  cdr_bytes: number
}
